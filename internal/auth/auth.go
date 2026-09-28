// Package auth implements OIDC login for the admin UI with signed session
// cookies.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/rs/zerolog"
	"golang.org/x/oauth2"
)

const (
	sessionCookie = "notary_session"
	flowCookie    = "notary_oidc"
	flowTTL       = 10 * time.Minute
)

// Config configures OIDC login.
type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	// AllowedEmails and AllowedGroups restrict who may sign in. If both are
	// empty, every user of the identity provider is an admin.
	AllowedEmails []string
	AllowedGroups []string
	GroupsClaim   string
	SessionSecret []byte
	SessionTTL    time.Duration
	// SecureCookies sets the Secure attribute on cookies.
	SecureCookies bool
}

// User is the authenticated admin.
type User struct {
	Subject string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
}

type session struct {
	User
	Expires int64 `json:"exp"`
}

type flow struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	Redirect string `json:"redirect"`
	Expires  int64  `json:"exp"`
}

// Authenticator handles login, logout and session verification. A nil
// *Authenticator means authentication is disabled.
type Authenticator struct {
	cfg      Config
	log      zerolog.Logger
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config
}

// New discovers the OIDC provider.
func New(ctx context.Context, cfg Config, log zerolog.Logger) (*Authenticator, error) {
	if cfg.Issuer == "" || cfg.ClientID == "" {
		return nil, errors.New("OIDC issuer and client ID are required")
	}
	if cfg.RedirectURL == "" {
		return nil, errors.New("OIDC redirect URL is required (set public-url or oidc.redirect-url)")
	}
	if len(cfg.SessionSecret) < 32 {
		return nil, errors.New("session secret must be at least 32 bytes")
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 12 * time.Hour
	}
	if cfg.GroupsClaim == "" {
		cfg.GroupsClaim = "groups"
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	} else if !slices.Contains(cfg.Scopes, oidc.ScopeOpenID) {
		cfg.Scopes = append([]string{oidc.ScopeOpenID}, cfg.Scopes...)
	}
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(dctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery: %w", err)
	}
	a := &Authenticator{
		cfg:      cfg,
		log:      log.With().Str("component", "auth").Logger(),
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       cfg.Scopes,
		},
	}
	if len(cfg.AllowedEmails) == 0 && len(cfg.AllowedGroups) == 0 {
		a.log.Warn().Msg("no allowed emails or groups configured: every user of the identity provider can administer the notary")
	}
	return a, nil
}

// Register adds /auth routes to mux.
func (a *Authenticator) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", a.login)
	mux.HandleFunc("GET /auth/callback", a.callback)
}

// UserFromRequest returns the user of a valid session cookie, or nil.
func (a *Authenticator) UserFromRequest(r *http.Request) *User {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	var s session
	if !a.verify(c.Value, &s) || time.Now().Unix() > s.Expires {
		return nil
	}
	return &s.User
}

// ClearSession removes the session cookie.
func (a *Authenticator) ClearSession(h http.Header) {
	c := &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: a.cfg.SecureCookies, SameSite: http.SameSiteLaxMode}
	h.Add("Set-Cookie", c.String())
}

func (a *Authenticator) login(w http.ResponseWriter, r *http.Request) {
	f := flow{
		State:    randomString(),
		Nonce:    randomString(),
		Verifier: oauth2.GenerateVerifier(),
		Redirect: safeRedirect(r.URL.Query().Get("redirect")),
		Expires:  time.Now().Add(flowTTL).Unix(),
	}
	http.SetCookie(w, &http.Cookie{
		Name: flowCookie, Value: a.sign(f), Path: "/auth/", MaxAge: int(flowTTL.Seconds()),
		HttpOnly: true, Secure: a.cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, a.oauth.AuthCodeURL(f.State, oidc.Nonce(f.Nonce), oauth2.S256ChallengeOption(f.Verifier)), http.StatusFound)
}

func (a *Authenticator) callback(w http.ResponseWriter, r *http.Request) {
	fail := func(status int, msg string, err error) {
		a.log.Warn().Err(err).Msg(msg)
		http.Error(w, msg, status)
	}
	c, err := r.Cookie(flowCookie)
	if err != nil {
		fail(http.StatusBadRequest, "login flow expired, please try again", err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Value: "", Path: "/auth/", MaxAge: -1, HttpOnly: true,
		Secure: a.cfg.SecureCookies, SameSite: http.SameSiteLaxMode})
	var f flow
	if !a.verify(c.Value, &f) || time.Now().Unix() > f.Expires {
		fail(http.StatusBadRequest, "login flow expired, please try again", nil)
		return
	}
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		fail(http.StatusUnauthorized, "identity provider returned an error: "+e+" "+q.Get("error_description"), nil)
		return
	}
	if q.Get("state") != f.State {
		fail(http.StatusBadRequest, "invalid login state", nil)
		return
	}
	tok, err := a.oauth.Exchange(r.Context(), q.Get("code"), oauth2.VerifierOption(f.Verifier))
	if err != nil {
		fail(http.StatusUnauthorized, "token exchange failed", err)
		return
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		fail(http.StatusUnauthorized, "identity provider returned no id_token", nil)
		return
	}
	idt, err := a.verifier.Verify(r.Context(), rawID)
	if err != nil {
		fail(http.StatusUnauthorized, "invalid id_token", err)
		return
	}
	if idt.Nonce != f.Nonce {
		fail(http.StatusUnauthorized, "invalid id_token nonce", nil)
		return
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		fail(http.StatusUnauthorized, "invalid id_token claims", err)
		return
	}
	// Merge userinfo claims (some providers only put groups/email there).
	if ui, err := a.provider.UserInfo(r.Context(), oauth2.StaticTokenSource(tok)); err == nil {
		var extra map[string]any
		if ui.Claims(&extra) == nil {
			for k, v := range extra {
				if _, ok := claims[k]; !ok {
					claims[k] = v
				}
			}
		}
	}

	user := User{Subject: idt.Subject, Email: claimString(claims, "email"),
		Name: firstNonEmpty(claimString(claims, "name"), claimString(claims, "preferred_username"), claimString(claims, "email"))}
	if !a.allowed(user, claims) {
		a.log.Warn().Str("subject", user.Subject).Str("email", user.Email).Msg("login denied: user not in allowed emails/groups")
		http.Error(w, "you are not allowed to administer this notary", http.StatusForbidden)
		return
	}
	s := session{User: user, Expires: time.Now().Add(a.cfg.SessionTTL).Unix()}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: a.sign(s), Path: "/", MaxAge: int(a.cfg.SessionTTL.Seconds()),
		HttpOnly: true, Secure: a.cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
	a.log.Info().Str("subject", user.Subject).Str("email", user.Email).Msg("admin signed in")
	http.Redirect(w, r, f.Redirect, http.StatusFound)
}

func (a *Authenticator) allowed(u User, claims map[string]any) bool {
	if len(a.cfg.AllowedEmails) == 0 && len(a.cfg.AllowedGroups) == 0 {
		return true
	}
	if u.Email != "" && claims["email_verified"] != false {
		for _, e := range a.cfg.AllowedEmails {
			if strings.EqualFold(e, u.Email) {
				return true
			}
		}
	}
	for _, g := range claimStrings(claims, a.cfg.GroupsClaim) {
		if slices.Contains(a.cfg.AllowedGroups, g) {
			return true
		}
	}
	return false
}

// sign serializes v as base64(json).base64(hmac).
func (a *Authenticator) sign(v any) string {
	b, _ := json.Marshal(v)
	payload := base64.RawURLEncoding.EncodeToString(b)
	return payload + "." + base64.RawURLEncoding.EncodeToString(a.mac(payload))
}

func (a *Authenticator) verify(value string, v any) bool {
	payload, sig, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, a.mac(payload)) {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}

func (a *Authenticator) mac(payload string) []byte {
	m := hmac.New(sha256.New, a.cfg.SessionSecret)
	m.Write([]byte(payload))
	return m.Sum(nil)
}

func randomString() string {
	return rand.Text()
}

// safeRedirect only allows local absolute paths to avoid open redirects.
func safeRedirect(p string) string {
	if p == "" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "/\\") {
		return "/"
	}
	if u, err := url.Parse(p); err != nil || u.Host != "" || u.Scheme != "" {
		return "/"
	}
	return p
}

func claimString(claims map[string]any, key string) string {
	s, _ := claims[key].(string)
	return s
}

func claimStrings(claims map[string]any, key string) []string {
	switch v := claims[key].(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
