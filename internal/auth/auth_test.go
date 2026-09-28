package auth

import (
	"net/http"
	"testing"
	"time"
)

func testAuthenticator(cfg Config) *Authenticator {
	cfg.SessionSecret = []byte("0123456789abcdef0123456789abcdef")
	return &Authenticator{cfg: cfg}
}

func TestSessionCookie(t *testing.T) {
	a := testAuthenticator(Config{})
	valid := a.sign(session{User: User{Subject: "u1", Email: "a@example.com"}, Expires: time.Now().Add(time.Hour).Unix()})
	expired := a.sign(session{User: User{Subject: "u1"}, Expires: time.Now().Add(-time.Hour).Unix()})
	other := testAuthenticator(Config{})
	other.cfg.SessionSecret = []byte("another-secret-another-secret-xx")
	forged := other.sign(session{User: User{Subject: "evil"}, Expires: time.Now().Add(time.Hour).Unix()})

	for name, c := range map[string]struct {
		value string
		ok    bool
	}{
		"valid": {valid, true}, "expired": {expired, false}, "forged": {forged, false},
		"tampered": {valid[:len(valid)-2] + "xx", false}, "garbage": {"nope", false},
	} {
		r := &http.Request{Header: http.Header{}}
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: c.value})
		if got := a.UserFromRequest(r) != nil; got != c.ok {
			t.Errorf("%s: authenticated = %v, want %v", name, got, c.ok)
		}
	}
}

func TestSafeRedirect(t *testing.T) {
	for in, want := range map[string]string{
		"":                   "/",
		"/registries/1":      "/registries/1",
		"/a?b=c":             "/a?b=c",
		"https://evil.test/": "/",
		"//evil.test/":       "/",
		"/\\evil.test":       "/",
		"relative":           "/",
	} {
		if got := safeRedirect(in); got != want {
			t.Errorf("safeRedirect(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAllowed(t *testing.T) {
	a := testAuthenticator(Config{AllowedEmails: []string{"Admin@Example.com"}, AllowedGroups: []string{"admins"}, GroupsClaim: "groups"})
	cases := []struct {
		name   string
		user   User
		claims map[string]any
		want   bool
	}{
		{"email", User{Email: "admin@example.com"}, map[string]any{}, true},
		{"unverified email", User{Email: "admin@example.com"}, map[string]any{"email_verified": false}, false},
		{"group", User{}, map[string]any{"groups": []any{"users", "admins"}}, true},
		{"nobody", User{Email: "x@example.com"}, map[string]any{"groups": []any{"users"}}, false},
	}
	for _, c := range cases {
		if got := a.allowed(c.user, c.claims); got != c.want {
			t.Errorf("%s: allowed = %v, want %v", c.name, got, c.want)
		}
	}
	if !testAuthenticator(Config{}).allowed(User{}, nil) {
		t.Error("no restrictions must allow everyone")
	}
}
