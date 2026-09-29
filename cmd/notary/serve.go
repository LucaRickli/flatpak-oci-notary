package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/lucarickli/flatpak-oci-notary/frontend"
	"github.com/lucarickli/flatpak-oci-notary/internal/auth"
	"github.com/lucarickli/flatpak-oci-notary/internal/indexer"
	"github.com/lucarickli/flatpak-oci-notary/internal/server"
	"github.com/lucarickli/flatpak-oci-notary/internal/version"
)

// serveFlags are the settings only the server uses.
var serveFlags = []flagSpec{
	{"listen", "listen", ":8080", "HTTP listen address"},
	{"public-url", "public-url", "", "externally visible base URL, e.g. https://flatpak.example.com (derived from requests if empty)"},
	{"trust-proxy", "trust-proxy", false, "trust X-Forwarded-Proto/Host when deriving the public URL"},
	{"index-max-age", "index-max-age", 5 * time.Minute, "Cache-Control max-age of served indexes"},
	{"sync.enabled", "sync-enabled", true, "run the sync scheduler in this process; disable it when a separate \"notary sync\" process syncs the registries"},
	checkIntervalFlag,
	{"auth.disabled", "auth-disabled", false, "disable admin authentication (development only!)"},
	{"oidc.issuer", "oidc-issuer", "", "OIDC issuer URL"},
	{"oidc.client-id", "oidc-client-id", "", "OIDC client ID"},
	{"oidc.client-secret", "oidc-client-secret", "", "OIDC client secret (omit for public clients)"},
	{"oidc.redirect-url", "oidc-redirect-url", "", "OIDC redirect URL (default: <public-url>/auth/callback)"},
	{"oidc.scopes", "oidc-scopes", []string{"openid", "profile", "email"}, "OIDC scopes"},
	{"oidc.allowed-emails", "oidc-allowed-emails", []string{}, "emails allowed to sign in (empty with no groups = everyone)"},
	{"oidc.allowed-groups", "oidc-allowed-groups", []string{}, "groups allowed to sign in"},
	{"oidc.groups-claim", "oidc-groups-claim", "groups", "claim holding the user's groups"},
	{"session.secret", "session-secret", "", "secret (>= 32 chars) for signing session cookies; random per start if empty (all replicas must share it)"},
	{"session.ttl", "session-ttl", 12 * time.Hour, "session lifetime"},
}

func newServeCmd(v *viper.Viper) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the notary server",
		Long: `Run the notary server: the flatpak indexes, the admin API and UI and, unless
sync.enabled is false, the sync scheduler.

Every flag can also be set in the config file (nested keys, e.g. oidc.issuer)
or through environment variables prefixed with NOTARY_, with dots and dashes
replaced by underscores (e.g. NOTARY_OIDC_CLIENT_SECRET).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			bindFlags(v, cmd.Flags(), serveFlags, databaseFlags, syncFlags)
			return runServe(cmd.Context(), v)
		},
	}
	defineFlags(cmd.Flags(), serveFlags, databaseFlags, syncFlags)
	return cmd
}

func runServe(ctx context.Context, v *viper.Viper) error {
	log, err := newLogger(v)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	publicURL := strings.TrimRight(v.GetString("public-url"), "/")
	if publicURL != "" && !strings.HasPrefix(publicURL, "https://") && !strings.HasPrefix(publicURL, "http://") {
		return fmt.Errorf("public-url must start with http:// or https://")
	}

	authn, err := setupAuth(ctx, v, publicURL, log)
	if err != nil {
		return err
	}

	st, dbCfg, err := openStore(ctx, v, log)
	if err != nil {
		return err
	}
	defer st.Close()

	// With the embedded syncer disabled this process never touches sync
	// leases: it neither resets nor claims syncs, it only records requests
	// for the "notary sync" process sharing the database.
	var idx *indexer.Indexer
	instance := "none (sync disabled)"
	indexerDone := make(chan struct{})
	if v.GetBool("sync.enabled") {
		idx = newIndexer(v, st, log, instanceID(v.GetString("sync.instance"), listenSuffix(v.GetString("listen"))))
		instance = idx.Owner()
		// Before anything can trigger a sync: release syncs a previous
		// process of this instance left running.
		idx.ResetInterrupted(ctx)
		go func() {
			defer close(indexerDone)
			idx.Run(ctx)
		}()
	} else {
		close(indexerDone)
		log.Info().Msg("embedded sync disabled: registries are synced by a separate \"notary sync\" process")
	}

	ui := frontend.FS()
	if ui == nil {
		log.Warn().Msg("built without UI (noui tag): only the API and indexes are served")
	}
	handler := server.New(st, idx, log, server.Options{
		PublicURL:     publicURL,
		IndexMaxAge:   v.GetDuration("index-max-age"),
		TrustProxy:    v.GetBool("trust-proxy"),
		UI:            ui,
		Authenticator: authn,
	})

	srv := &http.Server{
		Addr:              v.GetString("listen"),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// Flatpak's index queries and the admin API need little; the default
		// (1 MB) lets anonymous clients send huge query strings.
		MaxHeaderBytes: 64 << 10,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   5 * time.Minute,
		IdleTimeout:    2 * time.Minute,
		BaseContext:    func(net.Listener) context.Context { return ctx },
	}
	errc := make(chan error, 1)
	go func() {
		log.Info().Str("listen", srv.Addr).Str("version", version.Version).
			Str("database_driver", dbCfg.Driver).Str("database", dbCfg.Redacted()).
			Bool("embedded_sync", idx != nil).Str("instance", instance).
			Bool("auth", authn != nil).Str("public_url", publicURL).Msg("notary listening")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
		close(errc)
	}()

	select {
	case err := <-errc:
		stop()
		<-indexerDone
		return err
	case <-ctx.Done():
	}
	log.Info().Msg("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		log.Error().Err(err).Msg("http shutdown")
	}
	<-indexerDone
	return nil
}

func setupAuth(ctx context.Context, v *viper.Viper, publicURL string, log zerolog.Logger) (*auth.Authenticator, error) {
	issuer := v.GetString("oidc.issuer")
	if v.GetBool("auth.disabled") {
		if issuer != "" {
			return nil, errors.New("auth.disabled and oidc.issuer are mutually exclusive")
		}
		log.Warn().Msg("authentication is DISABLED: anyone who can reach the server can administer it")
		return nil, nil
	}
	if issuer == "" {
		return nil, errors.New("no admin authentication configured: set oidc.issuer and oidc.client-id (or pass --auth-disabled for local development)")
	}
	redirectURL := v.GetString("oidc.redirect-url")
	if redirectURL == "" && publicURL != "" {
		redirectURL = publicURL + "/auth/callback"
	}
	secret := []byte(v.GetString("session.secret"))
	if len(secret) == 0 {
		secret = []byte(rand.Text() + rand.Text())
		log.Warn().Msg("session.secret not set: using a random secret, sessions end on restart and do not work across replicas")
	}
	return auth.New(ctx, auth.Config{
		Issuer:        issuer,
		ClientID:      v.GetString("oidc.client-id"),
		ClientSecret:  v.GetString("oidc.client-secret"),
		RedirectURL:   redirectURL,
		Scopes:        v.GetStringSlice("oidc.scopes"),
		AllowedEmails: v.GetStringSlice("oidc.allowed-emails"),
		AllowedGroups: v.GetStringSlice("oidc.allowed-groups"),
		GroupsClaim:   v.GetString("oidc.groups-claim"),
		SessionSecret: secret,
		SessionTTL:    v.GetDuration("session.ttl"),
		SecureCookies: strings.HasPrefix(redirectURL, "https://"),
	}, log)
}
