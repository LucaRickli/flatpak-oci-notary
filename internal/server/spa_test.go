package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestSPAFallback(t *testing.T) {
	const index = "<html>index</html>"
	ui := fstest.MapFS{
		"index.html":           {Data: []byte(index)},
		"favicon.ico":          {Data: []byte("ico")},
		"_app/immutable/x.js":  {Data: []byte("js")},
		"_app/version.json":    {Data: []byte("{}")},
		"manifest.webmanifest": {Data: []byte("{}")},
	}
	h := spa(ui)
	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		return rec
	}

	// Client-side routes, including ones whose last segment looks like a
	// file extension (flatpak ids), get index.html.
	for _, p := range []string{
		"/",
		"/packages",
		"/packages/app/org.example.App",
		"/packages/runtime/org.fedoraproject.Platform",
		"/packages/runtime/org.fedoraproject.Platform?variant=01a0eaa7-5111-7993-8ff2-3d0ff8bb1711",
		"/packages/app/org.gnome.Calculator.Devel",
		"/images/01a0eaa7-5111-7993-8ff2-3d0ff8bb1711",
		"/registries/01a0eaa7-304a-789d-95b3-458a132b8a45",
	} {
		rec := get(p)
		if rec.Code != http.StatusOK || rec.Body.String() != index {
			t.Errorf("GET %s = %d %q, want 200 with index.html", p, rec.Code, rec.Body.String())
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("GET %s: Cache-Control %q, want no-cache", p, cc)
		}
	}

	// Missing assets are real 404s.
	for _, p := range []string{"/_app/immutable/missing.js", "/_app/immutable/chunks/x.css", "/favicon.png", "/robots.txt", "/Missing.JS"} {
		if rec := get(p); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
	}

	// Existing files are served, immutable assets with a long cache lifetime.
	rec := get("/_app/immutable/x.js")
	if rec.Code != http.StatusOK || rec.Body.String() != "js" {
		t.Fatalf("GET asset = %d %q", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("immutable asset Cache-Control %q", cc)
	}
	for _, p := range []string{"/favicon.ico", "/_app/version.json"} {
		rec := get(p)
		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("GET %s = %d, Cache-Control %q", p, rec.Code, rec.Header().Get("Cache-Control"))
		}
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/packages", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d, want 405", rec.Code)
	}
}
