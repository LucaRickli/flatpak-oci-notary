package indexer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/lucarickli/flatpak-oci-notary/internal/store"
	"github.com/lucarickli/flatpak-oci-notary/internal/store/storetest"
)

// blockingRegistry is a fake OCI registry whose catalog request blocks
// until released, to hold a sync in progress.
func blockingRegistry(t *testing.T) (url string, release func()) {
	t.Helper()
	url, release, _ = blockingRegistryNotify(t)
	return url, release
}

// blockingRegistryNotify is blockingRegistry, also signalling each catalog
// request on requested.
func blockingRegistryNotify(t *testing.T) (url string, release func(), requested <-chan struct{}) {
	t.Helper()
	gate := make(chan struct{})
	req := make(chan struct{}, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
		case "/v2/_catalog":
			select {
			case req <- struct{}{}:
			default:
			}
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"repositories":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	var once sync.Once
	return srv.URL, func() { once.Do(func() { close(gate) }) }, req
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSyncLease runs two indexer instances (as two replicas) on one database.
func TestSyncLease(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		ctx := context.Background()
		st := tg.Open(t)
		regURL, release := blockingRegistry(t)
		defer release()
		reg, err := st.CreateRegistry(ctx, &store.Registry{Name: "reg", URL: regURL, AuthType: store.AuthAnonymous, UseCatalog: true})
		if err != nil {
			t.Fatal(err)
		}
		log := zerolog.New(zerolog.NewTestWriter(t))
		opts := Options{LeaseDuration: 300 * time.Millisecond, HeartbeatInterval: 50 * time.Millisecond}
		a := New(st, log, Options{Owner: "a", LeaseDuration: opts.LeaseDuration, HeartbeatInterval: opts.HeartbeatInterval})
		b := New(tg.Open(t), log, Options{Owner: "b", LeaseDuration: opts.LeaseDuration, HeartbeatInterval: opts.HeartbeatInterval})

		// A starts a sync that blocks on the registry.
		done := make(chan bool, 1)
		go func() { ran, _ := a.SyncNow(ctx, reg.ID); done <- ran }()
		state := func() *store.Registry {
			r, err := st.GetRegistry(ctx, reg.ID)
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		waitFor(t, "sync to start", func() bool { return state().SyncState == store.SyncSyncing })
		if b.Trigger(ctx, reg.ID) {
			t.Fatal("second instance claimed a running sync")
		}
		if a.Trigger(ctx, reg.ID) {
			t.Fatal("same instance started a second sync")
		}
		// The heartbeat keeps the lease alive well beyond its duration.
		time.Sleep(3 * opts.LeaseDuration)
		if s := state(); s.SyncState != store.SyncSyncing || s.SyncOwner != "a" {
			t.Fatalf("lease not renewed: %+v", s)
		}
		if ran, _ := b.SyncNow(ctx, reg.ID); ran {
			t.Fatal("second instance claimed a renewed lease")
		}
		release()
		if !<-done {
			t.Fatal("first instance's sync reported not started")
		}
		if s := state(); s.SyncState != store.SyncError || s.SyncOwner != "" || !strings.Contains(s.LastSyncError, "no repositories") {
			t.Fatalf("after sync: %+v", s)
		}

		// A registry left "syncing" by a dead instance is reported as
		// interrupted and can be claimed once its lease expired.
		if ok, _ := st.ClaimSync(ctx, reg.ID, "dead", -time.Second); !ok {
			t.Fatal("claim for dead instance failed")
		}
		if s := state(); s.SyncState != store.SyncError || s.LastSyncError != store.LeaseExpiredError {
			t.Errorf("expired lease reported as %s %q", s.SyncState, s.LastSyncError)
		}
		if ran, err := b.SyncNow(ctx, reg.ID); !ran || err == nil || !strings.Contains(err.Error(), "no repositories") {
			t.Fatalf("expired lease not re-claimable or error not returned: ran=%v err=%v", ran, err)
		}
		if s := state(); s.SyncOwner != "" || s.SyncState != store.SyncError || s.LastSyncError == store.LeaseExpiredError {
			t.Errorf("after re-claimed sync: %+v", s)
		}
	})
}

// TestSyncLeaseLost checks that an instance whose lease was taken over
// aborts and does not overwrite the new owner's state.
func TestSyncLeaseLost(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		ctx := context.Background()
		st := tg.Open(t)
		regURL, release := blockingRegistry(t)
		defer release()
		reg, err := st.CreateRegistry(ctx, &store.Registry{Name: "reg", URL: regURL, AuthType: store.AuthAnonymous, UseCatalog: true})
		if err != nil {
			t.Fatal(err)
		}
		a := New(st, zerolog.New(zerolog.NewTestWriter(t)), Options{Owner: "a", LeaseDuration: 300 * time.Millisecond, HeartbeatInterval: 50 * time.Millisecond})
		done := make(chan error, 1)
		go func() { _, err := a.SyncNow(ctx, reg.ID); done <- err }()
		waitFor(t, "sync to start", func() bool {
			r, _ := st.GetRegistry(ctx, reg.ID)
			return r.SyncState == store.SyncSyncing
		})
		// Another instance takes over after the lease "expired".
		if err := st.DB().Model(&store.Registry{}).Where("id = ?", reg.ID).UpdateColumn("sync_lease_until", 1).Error; err != nil {
			t.Fatal(err)
		}
		if ok, _ := st.ClaimSync(ctx, reg.ID, "thief", time.Hour); !ok {
			t.Fatal("takeover failed")
		}
		select {
		case err := <-done:
			if !errors.Is(err, store.ErrLeaseLost) {
				t.Errorf("sync error after losing the lease: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("sync did not abort after losing its lease")
		}
		r, _ := st.GetRegistry(ctx, reg.ID)
		if r.SyncState != store.SyncSyncing || r.SyncOwner != "thief" {
			t.Errorf("state overwritten by the previous owner: %+v", r)
		}
	})
}

// run starts x.Run and returns a function stopping it and waiting for it.
func run(x *Indexer) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		x.Run(ctx)
	}()
	return func() {
		cancel()
		<-done
	}
}

// TestShutdownReleasesSync checks that a sync stopped by a (graceful)
// shutdown is not recorded as an attempt and is taken over by another
// replica on its next check, even when it was not due by its interval.
func TestShutdownReleasesSync(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		ctx := context.Background()
		st := tg.Open(t)
		regURL, release, requested := blockingRegistryNotify(t)
		defer release()
		reg, err := st.CreateRegistry(ctx, &store.Registry{Name: "reg", URL: regURL, AuthType: store.AuthAnonymous, UseCatalog: true, SyncIntervalMinutes: 60})
		if err != nil {
			t.Fatal(err)
		}
		// A sync completed recently, so the registry is not due.
		lastSync := time.Now().Add(-time.Minute).UTC().Truncate(time.Millisecond)
		if err := st.DB().Model(&store.Registry{}).Where("id = ?", reg.ID).
			UpdateColumns(map[string]any{"sync_state": store.SyncOK, "last_sync_at": &lastSync}).Error; err != nil {
			t.Fatal(err)
		}
		state := func() *store.Registry {
			r, err := st.GetRegistry(ctx, reg.ID)
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		log := zerolog.New(zerolog.NewTestWriter(t))
		opts := func(owner string) Options {
			return Options{Owner: owner, LeaseDuration: 300 * time.Millisecond, HeartbeatInterval: 50 * time.Millisecond, CheckInterval: 50 * time.Millisecond}
		}

		// Replica A runs a manual sync and shuts down in the middle of it.
		a := New(st, log, opts("a"))
		stopA := run(a)
		if !a.Trigger(ctx, reg.ID) {
			t.Fatal("manual sync not started")
		}
		select { // the sync is waiting for the catalog
		case <-requested:
		case <-time.After(5 * time.Second):
			t.Fatal("sync on a did not reach the registry")
		}
		stopA()
		r := state()
		if r.SyncState != store.SyncError || r.LastSyncError != store.ShutdownInterruptedError || r.SyncOwner != "" ||
			r.LastSyncAt == nil || !r.LastSyncAt.Equal(lastSync) {
			t.Fatalf("after shutdown: %+v (last sync %v)", r, lastSync)
		}

		// Replica B takes it over on its next check.
		b := New(tg.Open(t), log, opts("b"))
		stopB := run(b)
		defer stopB()
		waitFor(t, "takeover by b", func() bool { r := state(); return r.SyncState == store.SyncSyncing && r.SyncOwner == "b" })
		release()
		waitFor(t, "sync on b to finish", func() bool { return state().SyncState != store.SyncSyncing })
		if r := state(); !strings.Contains(r.LastSyncError, "no repositories") || r.LastSyncAt == nil || !r.LastSyncAt.After(lastSync) {
			t.Errorf("after takeover: %+v", r)
		}
	})
}

// TestRestartResetsInterruptedSyncs checks that a restarted instance does
// not wait for the lease of a sync its previous process left running: its
// own syncs (same identity) are reset, while syncs held by other instances
// (which may be alive, e.g. a standalone syncer sharing the database, also
// on SQLite) are left alone on every dialect.
func TestRestartResetsInterruptedSyncs(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		ctx := context.Background()
		st := tg.Open(t)
		regURL, release := blockingRegistry(t)
		release() // syncs finish at once
		create := func(name string) *store.Registry {
			r, err := st.CreateRegistry(ctx, &store.Registry{Name: name, URL: regURL, AuthType: store.AuthAnonymous, UseCatalog: true, SyncIntervalMinutes: 60})
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		own, other := create("own"), create("other")
		for id, owner := range map[string]string{own.ID: "me", other.ID: "someone-else"} {
			if ok, err := st.ClaimSync(ctx, id, owner, time.Hour); !ok || err != nil {
				t.Fatalf("claim: %v %v", ok, err)
			}
		}
		x := New(tg.Open(t), zerolog.New(zerolog.NewTestWriter(t)), Options{Owner: "me", CheckInterval: 50 * time.Millisecond})
		x.ResetInterrupted(ctx)
		stop := run(x)
		defer stop()
		synced := func(id string) func() bool {
			return func() bool {
				r, _ := st.GetRegistry(ctx, id)
				return r.LastSyncAt != nil && strings.Contains(r.LastSyncError, "no repositories")
			}
		}
		waitFor(t, "own interrupted sync to be retried", synced(own.ID))
		time.Sleep(200 * time.Millisecond)
		if r, _ := st.GetRegistry(ctx, other.ID); r.SyncState != store.SyncSyncing || r.SyncOwner != "someone-else" {
			t.Errorf("another instance's live sync was reset: %+v", r)
		}
	})
}
