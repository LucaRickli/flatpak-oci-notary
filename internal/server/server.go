// Package server wires the HTTP routes: the public flatpak index, the Connect
// admin API, OIDC login and the embedded UI.
package server

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"

	"github.com/lucarickli/flatpak-oci-notary/internal/api"
	"github.com/lucarickli/flatpak-oci-notary/internal/auth"
	"github.com/lucarickli/flatpak-oci-notary/internal/flatpakindex"
	"github.com/lucarickli/flatpak-oci-notary/internal/indexer"
	"github.com/lucarickli/flatpak-oci-notary/internal/store"
)

// Options configure the HTTP handler.
type Options struct {
	// PublicURL is the externally visible base URL. When empty it is derived
	// from each request (Host and X-Forwarded-Proto/Host headers).
	PublicURL     string
	IndexMaxAge   time.Duration
	TrustProxy    bool
	UI            fs.FS // may be nil
	Authenticator *auth.Authenticator
}

// New returns the root handler. x is nil when syncs run in a separate
// process (the API then only records sync requests).
func New(s *store.Store, x *indexer.Indexer, log zerolog.Logger, opts Options) http.Handler {
	baseURL := func(r *http.Request) string { return requestBaseURL(r, opts.PublicURL, opts.TrustProxy) }

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})

	flatpakindex.NewHandler(s, log, baseURL, opts.IndexMaxAge).Register(mux)

	a := api.New(s, x, opts.Authenticator, log)
	apiHandler := a.Handler()
	mux.Handle("/api/", rejectCrossSite(apiHandler))
	mux.HandleFunc("GET /icons/{id}", a.ServeIcon)

	if opts.Authenticator != nil {
		opts.Authenticator.Register(mux)
	}

	if opts.UI != nil {
		mux.Handle("/", spa(opts.UI))
	}

	var h http.Handler = mux
	h = withBaseURL(h, baseURL)
	h = securityHeaders(h)
	h = accessLog(h, log)
	return h
}

func withBaseURL(next http.Handler, baseURL func(*http.Request) string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(api.WithBaseURL(r.Context(), baseURL(r))))
	})
}

func requestBaseURL(r *http.Request, configured string, trustProxy bool) string {
	if configured != "" {
		return strings.TrimRight(configured, "/")
	}
	scheme, host := "http", r.Host
	if r.TLS != nil {
		scheme = "https"
	}
	if trustProxy {
		if p := r.Header.Get("X-Forwarded-Proto"); p == "https" || p == "http" {
			scheme = p
		}
		if h := r.Header.Get("X-Forwarded-Host"); h != "" {
			host = strings.TrimSpace(strings.Split(h, ",")[0])
		}
	}
	return scheme + "://" + host
}

// rejectCrossSite blocks state-changing cross-site requests (CSRF defense in
// depth; Connect requests are also never CORS-simple).
func rejectCrossSite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
				http.Error(w, "cross-site request rejected", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		if !strings.HasPrefix(r.URL.Path, "/repo/") {
			h.Set("X-Frame-Options", "DENY")
		}
		next.ServeHTTP(w, r)
	})
}

func accessLog(next http.Handler, log zerolog.Logger) http.Handler {
	h := hlog.AccessHandler(func(r *http.Request, status, size int, d time.Duration) {
		ev := hlog.FromRequest(r).Info()
		if status >= 500 {
			ev = hlog.FromRequest(r).Error()
		} else if r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/_app/") {
			ev = hlog.FromRequest(r).Debug()
		}
		ev.Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", status).
			Int("size", size).
			Dur("duration", d).
			Str("remote", r.RemoteAddr).
			Str("user_agent", r.UserAgent()).
			Msg("request")
	})(next)
	return hlog.NewHandler(log)(h)
}

func init() {
	// The scratch image has no /etc/mime.types; make sure the manifest gets
	// its proper type.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// rootAssetExts are the extensions of the static files at the root of the
// built UI (favicons, manifest, robots.txt). A missing single-segment path
// with one of them is a real 404, not a UI route.
var rootAssetExts = map[string]bool{
	".js": true, ".css": true, ".map": true, ".png": true, ".ico": true, ".svg": true,
	".webmanifest": true, ".json": true, ".txt": true, ".woff2": true,
}

// isAssetPath reports whether a path that does not exist in the UI is a
// missing asset (404) rather than a client-side route. Routes can end in
// what looks like a file extension (package pages end in a flatpak id such
// as org.gnome.Calculator), so only the generated asset directory and known
// root file types count as assets.
func isAssetPath(p string) bool {
	if strings.HasPrefix(p, "_app/") {
		return true
	}
	return !strings.Contains(p, "/") && rootAssetExts[strings.ToLower(path.Ext(p))]
}

// spa serves the built UI, falling back to index.html for client-side routes.
func spa(ui fs.FS) http.Handler {
	files := http.FileServerFS(ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if st, err := fs.Stat(ui, p); err == nil && !st.IsDir() {
			if strings.HasPrefix(p, "_app/immutable/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			files.ServeHTTP(w, r)
			return
		}
		// Unknown asset paths are real 404s; everything else is a UI route.
		if isAssetPath(p) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, ui, "index.html")
	})
}
