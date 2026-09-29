package server_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/rs/zerolog"

	"github.com/lucarickli/flatpak-oci-notary/internal/auth"
	notaryv1 "github.com/lucarickli/flatpak-oci-notary/internal/gen/notary/v1"
	"github.com/lucarickli/flatpak-oci-notary/internal/gen/notary/v1/notaryv1connect"
	"github.com/lucarickli/flatpak-oci-notary/internal/indexer"
	"github.com/lucarickli/flatpak-oci-notary/internal/server"
	"github.com/lucarickli/flatpak-oci-notary/internal/store/storetest"
)

// fakeIdP is a minimal OIDC provider: discovery, JWKS, an authorize endpoint
// that immediately redirects back with a code, and a PKCE-checking token endpoint.
type fakeIdP struct {
	srv    *httptest.Server
	key    *rsa.PrivateKey
	email  string
	groups []string

	mu    sync.Mutex
	codes map[string]authRequest
}

type authRequest struct {
	nonce, challenge, redirectURI string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	must(t, err)
	p := &fakeIdP{key: key, codes: map[string]authRequest{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                p.srv.URL,
			"authorization_endpoint":                p.srv.URL + "/authorize",
			"token_endpoint":                        p.srv.URL + "/token",
			"jwks_uri":                              p.srv.URL + "/keys",
			"userinfo_endpoint":                     p.srv.URL + "/userinfo",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /keys", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("code_challenge_method") != "S256" {
			http.Error(w, "PKCE required", http.StatusBadRequest)
			return
		}
		code := rand.Text()
		p.mu.Lock()
		p.codes[code] = authRequest{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirectURI: q.Get("redirect_uri")}
		p.mu.Unlock()
		http.Redirect(w, r, q.Get("redirect_uri")+"?"+url.Values{"code": {code}, "state": {q.Get("state")}}.Encode(), http.StatusFound)
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		p.mu.Lock()
		req, ok := p.codes[r.PostForm.Get("code")]
		delete(p.codes, r.PostForm.Get("code"))
		p.mu.Unlock()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != req.challenge {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: p.key}, (&jose.SignerOptions{}).WithHeader("kid", "k1"))
		if err != nil {
			t.Error(err)
		}
		idToken, err := jwt.Signed(signer).Claims(map[string]any{
			"iss": p.srv.URL, "sub": "user-1", "aud": "notary", "nonce": req.nonce,
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
			"email": p.email, "email_verified": true, "name": "Test User", "groups": p.groups,
		}).Serialize()
		if err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": idToken})
	})
	mux.HandleFunc("GET /userinfo", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"sub": "user-1"})
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func TestOIDCLogin(t *testing.T) {
	ctx := context.Background()
	idp := newFakeIdP(t)
	st := storetest.SQLite(t)
	logger := zerolog.New(zerolog.NewTestWriter(t))

	// The notary's URL must be known before creating the authenticator.
	var handler http.Handler
	notary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer notary.Close()
	a, err := auth.New(ctx, auth.Config{
		Issuer: idp.srv.URL, ClientID: "notary", ClientSecret: "secret",
		RedirectURL:   notary.URL + "/auth/callback",
		AllowedGroups: []string{"admins"},
		SessionSecret: []byte("0123456789abcdef0123456789abcdef"),
	}, logger)
	must(t, err)
	handler = server.New(st, indexer.New(st, logger, indexer.Options{}), logger, server.Options{Authenticator: a})

	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar}
	system := notaryv1connect.NewSystemServiceClient(browser, notary.URL+"/api")
	registries := notaryv1connect.NewRegistryServiceClient(browser, notary.URL+"/api")

	me, err := system.GetMe(ctx, connect.NewRequest(&notaryv1.GetMeRequest{}))
	must(t, err)
	if me.Msg.Authenticated || !me.Msg.AuthEnabled {
		t.Fatalf("before login: %+v", me.Msg)
	}
	if _, err := registries.ListRegistries(ctx, connect.NewRequest(&notaryv1.ListRegistriesRequest{})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("unauthenticated API call: %v, want Unauthenticated", err)
	}

	// A user outside the allowed groups is rejected.
	idp.email, idp.groups = "user@example.com", []string{"users"}
	res, err := browser.Get(notary.URL + "/auth/login?redirect=/registries")
	must(t, err)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("login of non-admin: status %d, want 403", res.StatusCode)
	}

	// An admin logs in and is redirected back to the requested page.
	idp.groups = []string{"admins"}
	res, err = browser.Get(notary.URL + "/auth/login?redirect=/registries")
	must(t, err)
	res.Body.Close()
	if res.Request.URL.Path != "/registries" {
		t.Errorf("redirected to %s, want /registries", res.Request.URL)
	}
	me, err = system.GetMe(ctx, connect.NewRequest(&notaryv1.GetMeRequest{}))
	must(t, err)
	if !me.Msg.Authenticated || me.Msg.User.GetEmail() != "user@example.com" || me.Msg.User.GetName() != "Test User" {
		t.Fatalf("after login: %+v", me.Msg)
	}
	if _, err := registries.ListRegistries(ctx, connect.NewRequest(&notaryv1.ListRegistriesRequest{})); err != nil {
		t.Fatalf("authenticated API call: %v", err)
	}

	// Cross-site requests are rejected even with a valid session.
	req, _ := http.NewRequest(http.MethodPost, notary.URL+"/api/notary.v1.RegistryService/ListRegistries", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	res, err = browser.Do(req)
	must(t, err)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site request status %d, want 403", res.StatusCode)
	}

	// A replayed callback (state already consumed) fails.
	res, err = browser.Get(notary.URL + "/auth/callback?code=x&state=y")
	must(t, err)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("replayed callback status %d, want 400", res.StatusCode)
	}

	_, err = system.Logout(ctx, connect.NewRequest(&notaryv1.LogoutRequest{}))
	must(t, err)
	if _, err := registries.ListRegistries(ctx, connect.NewRequest(&notaryv1.ListRegistriesRequest{})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("after logout: %v, want Unauthenticated", err)
	}

	// The public index needs no authentication.
	res, err = http.Get(notary.URL + "/healthz")
	must(t, err)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("healthz status %d", res.StatusCode)
	}
}
