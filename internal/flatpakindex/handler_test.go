package flatpakindex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/lucarickli/flatpak-oci-notary/internal/store"
	"github.com/lucarickli/flatpak-oci-notary/internal/store/storetest"
)

func testImage(repo, ref string) *store.Image {
	return &store.Image{
		Repository: repo, Digest: "sha256:" + repo + ref, MediaType: "application/vnd.oci.image.manifest.v1+json",
		OS: "linux", Architecture: "amd64", Tags: []string{"latest"}, Ref: ref,
		Labels: map[string]string{"org.flatpak.ref": ref}, IndexedAt: time.Now(),
	}
}

func serve(t *testing.T, h *Handler, slug string) (names []string, etag string) {
	t.Helper()
	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/repo/"+slug+"/index/static?architecture=amd64&os=linux&tag=latest", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET index: %d %s", rec.Code, rec.Body)
	}
	var resp struct{ Results []struct{ Name string } }
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	for _, r := range resp.Results {
		names = append(names, r.Name)
	}
	return names, rec.Header().Get("ETag")
}

// TestCacheInvalidationAcrossInstances checks that a write through one Store
// invalidates the index cache of a handler on another Store sharing the
// database, as happens between replicas.
func TestCacheInvalidationAcrossInstances(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		ctx := context.Background()
		a, b := tg.Open(t), tg.Open(t)
		log := zerolog.Nop()
		base := func(*http.Request) string { return "https://notary.test" }
		hA, hB := NewHandler(a, log, base, time.Minute), NewHandler(b, log, base, time.Minute)

		reg, err := a.CreateRegistry(ctx, &store.Registry{Name: "reg", URL: "https://reg.example", AuthType: store.AuthAnonymous, UseCatalog: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := a.ReplaceRegistryImages(ctx, reg.ID, "", []store.RepositoryResult{
			{Repository: "apps/x", Images: []*store.Image{testImage("apps/x", "app/org.example.X/x86_64/stable")}},
			{Repository: "apps/y", Images: []*store.Image{testImage("apps/y", "app/org.example.Y/x86_64/stable")}},
		}); err != nil {
			t.Fatal(err)
		}
		repo, err := a.CreateRepository(ctx, &store.Repository{Slug: "r", RegistryID: reg.ID, Sources: []store.Source{{RepositoryPattern: "*"}}})
		if err != nil {
			t.Fatal(err)
		}

		names, etag := serve(t, hB, "r")
		if len(names) != 2 {
			t.Fatalf("initial index on B: %v", names)
		}
		if _, again := serve(t, hB, "r"); again != etag || len(hB.cache) != 1 {
			t.Errorf("second request on B not served from cache (etag %s vs %s, %d entries)", again, etag, len(hB.cache))
		}

		// A repository change on A must be visible through B.
		repo.Sources = []store.Source{{RepositoryPattern: "apps/x"}}
		if _, err := a.UpdateRepository(ctx, repo); err != nil {
			t.Fatal(err)
		}
		if names, _ := serve(t, hB, "r"); len(names) != 1 || names[0] != "apps/x" {
			t.Errorf("after repository update on A, B serves %v", names)
		}
		// So must an image change (a sync) on A.
		if err := a.ReplaceRegistryImages(ctx, reg.ID, "", []store.RepositoryResult{
			{Repository: "apps/x", Images: []*store.Image{testImage("apps/x", "app/org.example.X/x86_64/stable"), testImage("apps/x", "app/org.example.X2/x86_64/stable")}},
		}); err != nil {
			t.Fatal(err)
		}
		if names, _ := serve(t, hB, "r"); len(names) != 1 {
			t.Errorf("after image replace on A, B serves %v", names)
		} else if n, _ := serve(t, hA, "r"); len(n) != 1 {
			t.Errorf("A serves %v", n)
		}
		var body struct {
			Results []struct{ Images []struct{ Digest string } }
		}
		mux := http.NewServeMux()
		hB.Register(mux)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/repo/r/index/static", nil))
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Results) != 1 || len(body.Results[0].Images) != 2 {
			t.Errorf("after image replace on A, B serves %s (%v)", rec.Body, err)
		}
		// Deleting the repository on A makes it 404 on B.
		if err := a.DeleteRepository(ctx, repo.ID); err != nil {
			t.Fatal(err)
		}
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/repo/r/index/static", nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("deleted repository on B: %d", rec.Code)
		}
	})
}

// TestOwnWritesBypassGenerationCache checks that an instance caching the
// generation still sees its own writes immediately.
func TestOwnWritesBypassGenerationCache(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		ctx := context.Background()
		cfg := tg.Config
		cfg.GenerationCacheTTL = time.Hour
		s, err := store.Open(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		h := NewHandler(s, zerolog.Nop(), func(*http.Request) string { return "https://notary.test" }, time.Minute)
		reg, err := s.CreateRegistry(ctx, &store.Registry{Name: "reg", URL: "https://reg.example", AuthType: store.AuthAnonymous, UseCatalog: true})
		if err != nil {
			t.Fatal(err)
		}
		repo, err := s.CreateRepository(ctx, &store.Repository{Slug: "r", RegistryID: reg.ID, Sources: []store.Source{{RepositoryPattern: "*"}}})
		if err != nil {
			t.Fatal(err)
		}
		if names, _ := serve(t, h, "r"); len(names) != 0 {
			t.Fatalf("empty repository serves %v", names)
		}
		if err := s.ReplaceRegistryImages(ctx, reg.ID, "", []store.RepositoryResult{
			{Repository: "apps/x", Images: []*store.Image{testImage("apps/x", "app/org.example.X/x86_64/stable")}},
		}); err != nil {
			t.Fatal(err)
		}
		if names, _ := serve(t, h, "r"); len(names) != 1 {
			t.Errorf("own image write not visible: %v", names)
		}
		repo.Sources = nil
		if _, err := s.UpdateRepository(ctx, repo); err != nil {
			t.Fatal(err)
		}
		if names, _ := serve(t, h, "r"); len(names) != 0 {
			t.Errorf("own repository write not visible: %v", names)
		}
	})
}

// TestServesCachedIndexWhileDatabaseDown checks that cached indexes are
// still served when the generation cannot be read, while requests that
// need the database fail.
func TestServesCachedIndexWhileDatabaseDown(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		ctx := context.Background()
		s := tg.Open(t) // generation cache disabled: every request reads it
		h := NewHandler(s, zerolog.Nop(), func(*http.Request) string { return "https://notary.test" }, time.Minute)
		reg, err := s.CreateRegistry(ctx, &store.Registry{Name: "reg", URL: "https://reg.example", AuthType: store.AuthAnonymous, UseCatalog: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ReplaceRegistryImages(ctx, reg.ID, "", []store.RepositoryResult{
			{Repository: "apps/x", Images: []*store.Image{testImage("apps/x", "app/org.example.X/x86_64/stable")}},
		}); err != nil {
			t.Fatal(err)
		}
		for _, slug := range []string{"cached", "uncached"} {
			if _, err := s.CreateRepository(ctx, &store.Repository{Slug: slug, RegistryID: reg.ID, Sources: []store.Source{{RepositoryPattern: "*"}}}); err != nil {
				t.Fatal(err)
			}
		}
		names, etag := serve(t, h, "cached")
		if len(names) != 1 {
			t.Fatalf("initial index: %v", names)
		}

		if err := s.Close(); err != nil { // the database goes away
			t.Fatal(err)
		}
		if names, again := serve(t, h, "cached"); len(names) != 1 || again != etag {
			t.Errorf("cached index while the database is down: %v %s", names, again)
		}
		mux := http.NewServeMux()
		h.Register(mux)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/repo/uncached/index/static?architecture=amd64&os=linux&tag=latest", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("uncached index while the database is down: %d", rec.Code)
		}
	})
}

// TestBuildsUncachedIndexWhenGenerationUnreadable checks that a failed
// generation read (e.g. a timeout behind a long write) does not fail
// requests the database can still answer; they are just not cached.
func TestBuildsUncachedIndexWhenGenerationUnreadable(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		ctx := context.Background()
		s := tg.Open(t)
		h := NewHandler(s, zerolog.Nop(), func(*http.Request) string { return "https://notary.test" }, time.Minute)
		reg, err := s.CreateRegistry(ctx, &store.Registry{Name: "reg", URL: "https://reg.example", AuthType: store.AuthAnonymous, UseCatalog: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ReplaceRegistryImages(ctx, reg.ID, "", []store.RepositoryResult{
			{Repository: "apps/x", Images: []*store.Image{testImage("apps/x", "app/org.example.X/x86_64/stable")}},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateRepository(ctx, &store.Repository{Slug: "r", RegistryID: reg.ID, Sources: []store.Source{{RepositoryPattern: "*"}}}); err != nil {
			t.Fatal(err)
		}
		var failGeneration atomic.Bool
		if err := s.DB().Callback().Query().Before("gorm:query").Register("test:fail_generation", func(db *gorm.DB) {
			if db.Statement.Table == "index_state" && failGeneration.Load() {
				_ = db.AddError(errors.New("generation read timed out"))
			}
		}); err != nil {
			t.Fatal(err)
		}
		failGeneration.Store(true)
		if names, _ := serve(t, h, "r"); len(names) != 1 {
			t.Errorf("index while the generation is unreadable: %v", names)
		}
		if len(h.cache) != 0 {
			t.Errorf("index built at an unknown generation was cached (%d entries)", len(h.cache))
		}
	})
}

// TestCacheBounded checks that anonymous requests with attacker-chosen
// filter values cannot grow the index cache: implausible values are not
// cached, keys have a fixed size and the number of entries is capped.
func TestCacheBounded(t *testing.T) {
	ctx := context.Background()
	s := storetest.SQLite(t)
	h := NewHandler(s, zerolog.Nop(), func(*http.Request) string { return "https://notary.test" }, time.Minute)
	reg, err := s.CreateRegistry(ctx, &store.Registry{Name: "reg", URL: "https://reg.example", AuthType: store.AuthAnonymous, UseCatalog: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceRegistryImages(ctx, reg.ID, "", []store.RepositoryResult{
		{Repository: "apps/x", Images: []*store.Image{testImage("apps/x", "app/org.example.X/x86_64/stable")}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRepository(ctx, &store.Repository{Slug: "r", RegistryID: reg.ID, Sources: []store.Source{{RepositoryPattern: "*"}}}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Register(mux)
	get := func(query string) {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/repo/r/index/static?"+query, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET index: %d", rec.Code)
		}
	}
	entries := func() (n, keyBytes int) {
		h.mu.Lock()
		defer h.mu.Unlock()
		for k := range h.cache {
			keyBytes += len(k)
		}
		return len(h.cache), keyBytes
	}

	long := strings.Repeat("x", 100<<10)
	for i := range 20 {
		get(fmt.Sprintf("label%%3Aorg.flatpak.ref%%3Aexists=1&tag=%s%d", long, i))
		get(fmt.Sprintf("tag=latest&architecture=%s%d", long, i))
		get(fmt.Sprintf("tag=la%%00test%d", i))
	}
	if n, _ := entries(); n != 0 {
		t.Errorf("%d entries cached for implausible filter values", n)
	}

	for i := range maxCacheEntries + 50 {
		get(fmt.Sprintf("label%%3Aorg.flatpak.ref%%3Aexists=1&architecture=amd64&os=linux&tag=t%d", i))
	}
	n, keyBytes := entries()
	if n == 0 || n > maxCacheEntries {
		t.Errorf("%d cache entries, want 1..%d", n, maxCacheEntries)
	}
	if keyBytes != n*64 {
		t.Errorf("cache keys use %d bytes for %d entries, want fixed-size keys", keyBytes, n)
	}
	h.mu.Lock()
	accounted := h.cacheBytes
	h.mu.Unlock()
	if accounted < n*(64+entryOverhead) {
		t.Errorf("cacheBytes %d does not account for keys and overhead of %d entries", accounted, n)
	}

	// The flatpak query itself is still cached and served from the cache.
	get("label%3Aorg.flatpak.ref%3Aexists=1&architecture=amd64&os=linux&tag=latest")
	if _, ok := h.cache[cacheKey("r", ParseFilter(mustQuery(t, "label%3Aorg.flatpak.ref%3Aexists=1&architecture=amd64&os=linux&tag=latest")))]; !ok {
		t.Error("flatpak's query was not cached")
	}
}

func mustQuery(t *testing.T, q string) url.Values {
	t.Helper()
	v, err := url.ParseQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
