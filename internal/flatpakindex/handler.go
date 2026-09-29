package flatpakindex

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/lucarickli/flatpak-oci-notary/internal/store"
)

// URLs are the public URLs of a repository.
type URLs struct {
	Remote      string
	Flatpakrepo string
	Index       string
}

// RepositoryURLs derives the public URLs of a repository from the server's
// base URL (e.g. "https://flatpak.example.com").
func RepositoryURLs(baseURL, slug string) URLs {
	base := strings.TrimRight(baseURL, "/")
	return URLs{
		Remote:      "oci+" + base + "/repo/" + slug,
		Flatpakrepo: base + "/repo/" + slug + ".flatpakrepo",
		Index:       base + "/repo/" + slug + "/index/static",
	}
}

// Handler serves repository indexes and .flatpakrepo files.
type Handler struct {
	store   *store.Store
	log     zerolog.Logger
	baseURL func(*http.Request) string
	maxAge  time.Duration

	mu         sync.Mutex
	cache      map[string]*cached
	cacheBytes int
	// staleWarned is when serving from cache during a database outage was
	// last logged (Unix nanoseconds), to rate-limit the warning.
	staleWarned atomic.Int64
}

type cached struct {
	generation uint64
	etag       string
	raw        []byte
	gz         []byte
}

const (
	// maxCacheBytes bounds the memory used for cached indexes (raw + gzip
	// + key and bookkeeping, see entryOverhead).
	maxCacheBytes = 256 << 20
	// maxCacheEntries bounds the number of cached indexes, whatever their
	// size.
	maxCacheEntries = 1024
	// entryOverhead approximates the memory of a cache entry besides its
	// bodies (map slot, struct, ETag).
	entryOverhead = 256
	// maxFilterValue bounds a tag, architecture or OS value of a cacheable
	// request; flatpak sends short names.
	maxFilterValue = 128
)

// cacheable reports whether a filter is worth caching. Only request shapes
// flatpak itself sends are cached (at most one plausible tag, architecture
// and OS), so arbitrary query variations from anonymous clients cannot fill
// the cache.
func cacheable(f Filter) bool {
	if len(f.LabelEquals) != 0 || len(f.Tags) > 1 || len(f.Architectures) > 1 || len(f.OS) > 1 ||
		!(len(f.LabelExists) == 0 || (len(f.LabelExists) == 1 && f.LabelExists[0] == "org.flatpak.ref")) {
		return false
	}
	for _, values := range [][]string{f.Tags, f.Architectures, f.OS} {
		for _, v := range values {
			if !plausibleName(v) {
				return false
			}
		}
	}
	return true
}

// plausibleName reports whether v looks like a tag, architecture or OS
// name: short, of letters, digits and ._- only.
func plausibleName(v string) bool {
	if v == "" || len(v) > maxFilterValue {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// cacheKey is the fixed-size cache key of a repository index request.
func cacheKey(slug string, f Filter) string {
	sum := sha256.Sum256([]byte(slug + "?" + f.CacheKey()))
	return hex.EncodeToString(sum[:])
}

// entrySize is the memory a cache entry is accounted for.
func entrySize(key string, c *cached) int {
	return len(key) + len(c.raw) + len(c.gz) + entryOverhead
}

func NewHandler(s *store.Store, log zerolog.Logger, baseURL func(*http.Request) string, maxAge time.Duration) *Handler {
	return &Handler{
		store:   s,
		log:     log.With().Str("component", "index").Logger(),
		baseURL: baseURL,
		maxAge:  maxAge,
		cache:   map[string]*cached{},
	}
}

// Register adds the public routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /repo/{slug}/index/static", h.serveIndex)
	mux.HandleFunc("GET /repo/{slug}/index/dynamic", h.serveIndex)
	mux.HandleFunc("GET /repo/{file}", h.serveRepoFile)
}

func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	filter := ParseFilter(r.URL.Query())
	entry, err := h.index(r.Context(), slug, filter)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}
	if err != nil {
		h.log.Error().Err(err).Str("repository", slug).Msg("build index")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	hdr := w.Header()
	hdr.Set("ETag", entry.etag)
	hdr.Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(h.maxAge.Seconds())))
	hdr.Set("Vary", "Accept-Encoding")
	hdr.Set("Content-Type", "application/json")
	if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatches(inm, entry.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body := entry.raw
	if acceptsGzip(r) {
		hdr.Set("Content-Encoding", "gzip")
		body = entry.gz
	}
	hdr.Set("Content-Length", fmt.Sprint(len(body)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

func (h *Handler) index(ctx context.Context, slug string, f Filter) (*cached, error) {
	key := cacheKey(slug, f)
	// The generation lives in the database, so writes by other replicas
	// invalidate this cache too.
	gen, err := h.store.Generation(ctx)
	known := err == nil
	if !known {
		if ctx.Err() != nil {
			return nil, err
		}
		if c := h.fallback(key, err); c != nil {
			return c, nil
		}
		// Not cached: build it anyway (without caching, the generation is
		// unknown). That fails if the database is really down, and succeeds
		// if it was only slow, e.g. behind a long write on SQLite.
	} else {
		h.mu.Lock()
		if c, ok := h.cache[key]; ok && c.generation == gen {
			h.mu.Unlock()
			return c, nil
		}
		h.mu.Unlock()
	}

	repo, err := h.store.GetRepositoryBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	reg, err := h.store.GetRegistry(ctx, repo.RegistryID)
	if err != nil {
		return nil, err
	}
	images, err := h.store.RegistryImages(ctx, reg.ID, true)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(Build(reg.URL, Select(repo.Sources, f, images)))
	if err != nil {
		return nil, err
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write(raw)
	if err := zw.Close(); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	c := &cached{generation: gen, etag: `"` + hex.EncodeToString(sum[:16]) + `"`, raw: raw, gz: gz.Bytes()}

	if known && cacheable(f) {
		h.remember(key, c)
	}
	return c, nil
}

// fallback returns the cached index at the last known generation when the
// generation cannot be read (database unreachable or slow), or nil. While
// the database is down no replica can change the data, so the entry is
// still current.
func (h *Handler) fallback(key string, err error) *cached {
	last, ok := h.store.LastGeneration()
	if !ok {
		return nil
	}
	h.mu.Lock()
	c, hit := h.cache[key]
	h.mu.Unlock()
	if !hit || c.generation != last {
		return nil
	}
	now := time.Now().UnixNano()
	if prev := h.staleWarned.Load(); now-prev > int64(10*time.Second) && h.staleWarned.CompareAndSwap(prev, now) {
		h.log.Warn().Err(err).Msg("cannot read the index generation; serving cached indexes")
	}
	return c
}

func (h *Handler) remember(key string, c *cached) {
	size := entrySize(key, c)
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, v := range h.cache {
		if v.generation != c.generation {
			h.cacheBytes -= entrySize(k, v)
			delete(h.cache, k)
		}
	}
	if prev, ok := h.cache[key]; ok {
		h.cacheBytes -= entrySize(key, prev)
		delete(h.cache, key)
	}
	if h.cacheBytes+size > maxCacheBytes || len(h.cache) >= maxCacheEntries {
		clear(h.cache)
		h.cacheBytes = 0
	}
	if size <= maxCacheBytes {
		h.cache[key] = c
		h.cacheBytes += size
	}
}

func (h *Handler) serveRepoFile(w http.ResponseWriter, r *http.Request) {
	slug, ok := strings.CutSuffix(r.PathValue("file"), ".flatpakrepo")
	if !ok {
		http.NotFound(w, r)
		return
	}
	repo, err := h.store.GetRepositoryBySlug(r.Context(), slug)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}
	if err != nil {
		h.log.Error().Err(err).Str("repository", slug).Msg("load repository")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	urls := RepositoryURLs(h.baseURL(r), repo.Slug)
	var b strings.Builder
	b.WriteString("[Flatpak Repo]\n")
	writeKey(&b, "Title", cmpOr(repo.Title, repo.Slug))
	writeKey(&b, "Url", urls.Remote)
	writeKey(&b, "Homepage", repo.Homepage)
	writeKey(&b, "Comment", repo.Description)
	w.Header().Set("Content-Type", "application/vnd.flatpak.repo")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.flatpakrepo"`, repo.Slug))
	_, _ = w.Write([]byte(b.String()))
}

func writeKey(b *strings.Builder, key, value string) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\n", " "))
	if value != "" {
		b.WriteString(key + "=" + value + "\n")
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		enc, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(enc), "gzip") && strings.ReplaceAll(params, " ", "") != "q=0" {
			return true
		}
	}
	return false
}

func etagMatches(header, etag string) bool {
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimPrefix(strings.TrimSpace(t), "W/")
		if t == etag || t == "*" {
			return true
		}
	}
	return false
}
