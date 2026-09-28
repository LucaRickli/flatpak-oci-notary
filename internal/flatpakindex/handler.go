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
}

type cached struct {
	generation uint64
	etag       string
	raw        []byte
	gz         []byte
}

// maxCacheBytes bounds the memory used for cached indexes (raw + gzip).
const maxCacheBytes = 256 << 20

// cacheable reports whether a filter is worth caching. Only request shapes
// flatpak itself sends are cached, so arbitrary query variations from
// anonymous clients cannot fill the cache.
func cacheable(f Filter) bool {
	return len(f.LabelEquals) == 0 && len(f.Tags) <= 1 && len(f.Architectures) <= 1 && len(f.OS) <= 1 &&
		(len(f.LabelExists) == 0 || (len(f.LabelExists) == 1 && f.LabelExists[0] == "org.flatpak.ref"))
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
	gen := h.store.Generation()
	key := slug + "?" + f.CacheKey()
	h.mu.Lock()
	if c, ok := h.cache[key]; ok && c.generation == gen {
		h.mu.Unlock()
		return c, nil
	}
	h.mu.Unlock()

	repo, err := h.store.GetRepositoryBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	reg, err := h.store.GetRegistry(ctx, repo.RegistryID)
	if err != nil {
		return nil, err
	}
	rows, err := h.store.ListImages(ctx, store.ImageFilter{RegistryID: reg.ID, WithLabels: true})
	if err != nil {
		return nil, err
	}
	images := make([]*store.Image, len(rows))
	for i, row := range rows {
		images[i] = row.Image
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

	if cacheable(f) {
		h.remember(key, c)
	}
	return c, nil
}

func (h *Handler) remember(key string, c *cached) {
	size := len(c.raw) + len(c.gz)
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, v := range h.cache {
		if v.generation != c.generation {
			h.cacheBytes -= len(v.raw) + len(v.gz)
			delete(h.cache, k)
		}
	}
	if prev, ok := h.cache[key]; ok {
		h.cacheBytes -= len(prev.raw) + len(prev.gz)
		delete(h.cache, key)
	}
	if h.cacheBytes+size > maxCacheBytes {
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
