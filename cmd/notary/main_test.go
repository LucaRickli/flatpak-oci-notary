package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/rs/zerolog"

	"github.com/lucarickli/flatpak-oci-notary/internal/store"
)

var ctx = context.Background()

// ociRegistry serves an in-memory OCI registry holding one flatpak image.
func ociRegistry(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(srv.Close)
	img, err := random.Image(16, 1)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	cfg = cfg.DeepCopy()
	cfg.OS, cfg.Architecture = "linux", "amd64"
	cfg.Config.Labels = map[string]string{"org.flatpak.ref": "app/org.example.One/x86_64/stable"}
	if img, err = mutate.ConfigFile(img, cfg); err != nil {
		t.Fatal(err)
	}
	ref, err := name.ParseReference(strings.TrimPrefix(srv.URL, "http://")+"/apps/one:latest", name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(ref, img); err != nil {
		t.Fatal(err)
	}
	return srv.URL
}

func openStoreAt(t *testing.T, dataDir string) *store.Store {
	t.Helper()
	st, err := store.Open(ctx, store.Config{DSN: filepath.Join(dataDir, "notary.db"), GenerationCacheTTL: -1, Logger: zerolog.Nop()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func createRegistry(t *testing.T, st *store.Store, name, url string, interval int) *store.Registry {
	t.Helper()
	r, err := st.CreateRegistry(ctx, &store.Registry{Name: name, URL: url, AuthType: store.AuthAnonymous, UseCatalog: true, SyncIntervalMinutes: interval})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// execute runs the CLI with the given arguments against the data dir.
func execute(t *testing.T, ctx context.Context, dataDir string, args ...string) error {
	t.Helper()
	cmd := newRootCmd()
	cmd.SetArgs(append([]string{args[0], "--data-dir", dataDir, "--log-format", "console", "--log-level", "debug"}, args[1:]...))
	cmd.SetOut(io.Discard)
	return cmd.ExecuteContext(ctx)
}

func registryState(t *testing.T, st *store.Store, id string) *store.Registry {
	t.Helper()
	r, err := st.GetRegistry(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestSyncCommand covers the one-shot "notary sync": selection, --due, exit
// status on failures and skipping registries synced elsewhere.
func TestSyncCommand(t *testing.T) {
	dataDir := t.TempDir()
	st := openStoreAt(t, dataDir)
	good := createRegistry(t, st, "good", ociRegistry(t), 60)
	bad := createRegistry(t, st, "bad", "http://127.0.0.1:1", 60)
	manual := createRegistry(t, st, "manual", good.URL, 0)

	if err := execute(t, ctx, dataDir, "sync", "--registry", "good"); err != nil {
		t.Fatalf("sync good: %v", err)
	}
	if r := registryState(t, st, good.ID); r.SyncState != store.SyncOK || r.ImageCount != 1 || r.SyncRepositoriesDone != 1 || r.SyncRepositoriesTotal != 1 {
		t.Errorf("good after sync: %+v", r)
	}
	if r := registryState(t, st, bad.ID); r.SyncState != store.SyncNever {
		t.Errorf("unselected registry synced: %+v", r)
	}
	if err := execute(t, ctx, dataDir, "sync", "--registry", "nope"); err == nil || !strings.Contains(err.Error(), "unknown registry") {
		t.Errorf("unknown registry: %v", err)
	}
	// A failing registry fails the run; the others are still synced.
	err := execute(t, ctx, dataDir, "sync")
	if err == nil || !strings.Contains(err.Error(), "1 of 3 registries failed") {
		t.Errorf("sync all with a failing registry: %v", err)
	}
	badBefore := registryState(t, st, bad.ID)
	if badBefore.SyncState != store.SyncError || badBefore.LastSyncAt == nil {
		t.Errorf("bad after sync: %+v", badBefore)
	}
	if r := registryState(t, st, manual.ID); r.SyncState != store.SyncOK {
		t.Errorf("manual after sync all: %+v", r)
	}
	// --due: nothing is due right after a sync ...
	if err := execute(t, ctx, dataDir, "sync", "--due"); err != nil {
		t.Errorf("due run with nothing due: %v", err)
	}
	before := registryState(t, st, manual.ID)
	// ... until a sync is requested; the request is cleared by the run.
	if err := st.RequestSync(ctx, manual.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := execute(t, ctx, dataDir, "sync", "--due"); err != nil {
		t.Errorf("due run with a request: %v", err)
	}
	if r := registryState(t, st, manual.ID); r.SyncRequested || !r.LastSyncAt.After(*before.LastSyncAt) {
		t.Errorf("requested registry not synced by --due: %+v", r)
	}
	if r := registryState(t, st, bad.ID); r.LastSyncAt == nil || !r.LastSyncAt.Equal(*badBefore.LastSyncAt) {
		t.Errorf("bad registry (recently failed, not due) touched by --due: %+v", r)
	}
	// A registry synced by another process is skipped, not failed.
	if ok, _ := st.ClaimSync(ctx, good.ID, "someone-else", time.Hour); !ok {
		t.Fatal("claim")
	}
	if err := execute(t, ctx, dataDir, "sync", "--registry", "good", "--registry", "manual"); err != nil {
		t.Errorf("run with a registry synced elsewhere: %v", err)
	}
	if r := registryState(t, st, good.ID); r.SyncState != store.SyncSyncing || r.SyncOwner != "someone-else" {
		t.Errorf("registry synced elsewhere touched: %+v", r)
	}
	if err := execute(t, ctx, dataDir, "sync", "--watch", "--due"); err == nil {
		t.Error("--watch with --due accepted")
	}
}

// TestServeSyncDisabled checks that a server with the embedded syncer
// disabled neither resets nor claims syncs and only records requests, while
// one with it enabled does both.
func TestServeSyncDisabled(t *testing.T) {
	dataDir := t.TempDir()
	st := openStoreAt(t, dataDir)
	url := ociRegistry(t)
	stale := createRegistry(t, st, "stale", url, 60)
	requested := createRegistry(t, st, "requested", url, 0)
	// The identity a server listening on port 0 gets, from a previous run.
	owner := instanceID("", ":0")
	if owner == "" {
		t.Skip("no hostname")
	}
	if ok, _ := st.ClaimSync(ctx, stale.ID, owner, time.Hour); !ok {
		t.Fatal("claim")
	}
	if err := st.RequestSync(ctx, requested.ID); err != nil {
		t.Fatal(err)
	}

	serve := func(enabled bool) {
		t.Helper()
		sctx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() {
			done <- execute(t, sctx, dataDir, "serve", "--listen", "127.0.0.1:0", "--auth-disabled",
				"--sync-enabled="+map[bool]string{true: "true", false: "false"}[enabled], "--sync-check-interval", "50ms")
		}()
		if enabled {
			deadline := time.Now().Add(10 * time.Second)
			for registryState(t, st, requested.ID).SyncState != store.SyncOK && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
		} else {
			time.Sleep(500 * time.Millisecond)
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("serve: %v", err)
		}
	}

	serve(false)
	if r := registryState(t, st, stale.ID); r.SyncState != store.SyncSyncing || r.SyncOwner != owner {
		t.Errorf("server without embedded sync reset a sync: %+v", r)
	}
	if r := registryState(t, st, requested.ID); r.SyncState != store.SyncNever || !r.SyncRequested {
		t.Errorf("server without embedded sync claimed a requested sync: %+v", r)
	}

	serve(true)
	if r := registryState(t, st, stale.ID); r.SyncState == store.SyncSyncing && r.SyncOwner == owner {
		t.Errorf("server with embedded sync did not reset its own interrupted sync: %+v", r)
	}
	if r := registryState(t, st, requested.ID); r.SyncState != store.SyncOK || r.SyncRequested {
		t.Errorf("server with embedded sync did not run the requested sync: %+v", r)
	}
}

func TestInstanceID(t *testing.T) {
	host, err := os.Hostname()
	if err != nil || host == "" {
		t.Skip("no hostname")
	}
	if got := instanceID(" configured ", ":8080"); got != "configured" {
		t.Errorf("configured id: %q", got)
	}
	if got := instanceID("", listenSuffix(":8080")); got != host+":8080" {
		t.Errorf("server id: %q", got)
	}
	if got := instanceID("", listenSuffix("bad")); got != host {
		t.Errorf("server id without port: %q", got)
	}
	if got := instanceID("", ":sync-42"); got != host+":sync-42" {
		t.Errorf("sync id: %q", got)
	}
	// The default sync suffix is unique per process start, even where every
	// process is pid 1 (containers).
	a, b := syncSuffix(), syncSuffix()
	if a == b || !strings.HasPrefix(a, fmt.Sprintf(":sync-%d-", os.Getpid())) || len(a) > 32 {
		t.Errorf("sync suffixes %q and %q", a, b)
	}
	if got := instanceID("", a); !strings.HasSuffix(got, a) {
		t.Errorf("sync id %q does not end in %q", got, a)
	}
	long := strings.Repeat("h", 100)
	if got := instanceID(long, ":8080"); len(got) != store.MaxSyncOwnerLength {
		t.Errorf("long configured id not cut: %d", len(got))
	}
	if got := instanceID(long, ""); got != long[:store.MaxSyncOwnerLength] {
		t.Errorf("long configured id: %q", got)
	}
}
