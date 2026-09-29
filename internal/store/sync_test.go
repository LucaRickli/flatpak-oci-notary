package store_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/lucarickli/flatpak-oci-notary/internal/store"
	"github.com/lucarickli/flatpak-oci-notary/internal/store/storetest"
)

// TestSyncRequests covers the sync request flag and the progress counters:
// set on request or by the caller of create/update, cleared by a claim,
// none of it touching the index generation.
func TestSyncRequests(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		s := tg.Open(t)
		plain := newRegistry(t, s, "plain")
		if plain.SyncRequested {
			t.Error("registry created without a request is requested")
		}
		asked, err := s.CreateRegistry(ctx, &store.Registry{Name: "asked", URL: "https://asked.example", AuthType: store.AuthAnonymous, UseCatalog: true,
			SyncRequested: true, SyncRepositoriesDone: 7, SyncRepositoriesTotal: 9})
		must(t, err)
		if !asked.SyncRequested || asked.SyncRepositoriesDone != 0 || asked.SyncRepositoriesTotal != 0 {
			t.Errorf("created with a request: %+v", asked)
		}
		g0, err := s.Generation(ctx)
		must(t, err)

		must(t, s.RequestSync(ctx, plain.ID))
		if r, _ := s.GetRegistry(ctx, plain.ID); !r.SyncRequested || r.SyncState != store.SyncNever {
			t.Errorf("after request: %+v", r)
		}
		if err := s.RequestSync(ctx, store.NewID()); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("request for an unknown registry: %v", err)
		}
		if _, err := s.SetSyncTotal(ctx, plain.ID, "o", 3); err != nil {
			t.Fatal(err)
		} else if r, _ := s.GetRegistry(ctx, plain.ID); r.SyncRepositoriesTotal != 0 {
			t.Errorf("total set without a lease: %+v", r)
		}

		// The claim clears the request and resets the progress.
		claimed, err := s.ClaimSync(ctx, plain.ID, "o", time.Minute)
		must(t, err)
		if !claimed {
			t.Fatal("claim failed")
		}
		ok, err := s.SetSyncTotal(ctx, plain.ID, "o", 3)
		must(t, err)
		if !ok {
			t.Error("owner could not set the total")
		}
		if ok, _ := s.SetSyncTotal(ctx, plain.ID, "other", 5); ok {
			t.Error("non-owner set the total")
		}
		r, _ := s.GetRegistry(ctx, plain.ID)
		if r.SyncRequested || r.SyncRepositoriesDone != 0 || r.SyncRepositoriesTotal != 3 {
			t.Errorf("after claim and total: %+v", r)
		}
		// A request during the sync survives it, so the registry is due again.
		must(t, s.RequestSync(ctx, plain.ID))
		_, err = s.FinishSync(ctx, plain.ID, "o", time.Now(), time.Second, nil)
		must(t, err)
		r, _ = s.GetRegistry(ctx, plain.ID)
		if !r.SyncRequested || r.SyncState != store.SyncOK || r.SyncRepositoriesTotal != 3 {
			t.Errorf("after finish with a pending request: %+v", r)
		}
		claimed, err = s.ClaimSync(ctx, plain.ID, "o", time.Minute)
		must(t, err)
		if r, _ = s.GetRegistry(ctx, plain.ID); !claimed || r.SyncRequested || r.SyncRepositoriesTotal != 0 {
			t.Errorf("after second claim: %+v", r)
		}

		// Update writes the flag as given.
		asked.SyncRequested = false
		asked, err = s.UpdateRegistry(ctx, asked, false)
		must(t, err)
		if asked.SyncRequested {
			t.Error("update did not clear the flag")
		}
		asked.SyncRequested = true
		if asked, err = s.UpdateRegistry(ctx, asked, false); err != nil || !asked.SyncRequested {
			t.Errorf("update did not set the flag: %v %+v", err, asked)
		}

		// Bookkeeping never invalidates indexes (the update above does).
		g1, _ := s.Generation(ctx)
		if g1 != g0+2 {
			t.Errorf("generation %d -> %d, want exactly the two updates", g0, g1)
		}
	})
}

// TestWriteSyncBatch covers incremental writes: per-repository replacement,
// one generation bump per batch that wrote something, progress and lease
// renewal under the owner check, untouched repositories outside the batch
// and the separate stale-repository removal.
func TestWriteSyncBatch(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		s := tg.Open(t)
		r := newRegistry(t, s, "reg")
		must(t, s.ReplaceRegistryImages(ctx, r.ID, "", ok(
			repo("a/keep", img("a/keep", "app/org.example.Keep/x86_64/stable", "amd64")),
			repo("a/gone", img("a/gone", "app/org.example.Gone/x86_64/stable", "amd64")),
			repo("a/flaky", img("a/flaky", "app/org.example.Flaky/x86_64/stable", "amd64")),
		)))
		gen := func() uint64 {
			g, err := s.Generation(ctx)
			must(t, err)
			return g
		}
		repos := func() []string {
			images, err := s.RegistryImages(ctx, r.ID, false)
			must(t, err)
			var out []string
			for _, im := range images {
				out = append(out, im.Repository+":"+im.Ref)
			}
			slices.Sort(out)
			return out
		}
		claimed, err := s.ClaimSync(ctx, r.ID, "o", time.Second)
		must(t, err)
		if !claimed {
			t.Fatal("claim failed")
		}
		before, _ := s.GetRegistry(ctx, r.ID)
		g0 := gen()

		// Batch 1: keep gets a new image (old one gone), new appears, flaky
		// failed; gone is not in the batch and stays.
		results := ok(
			repo("a/keep", img("a/keep", "app/org.example.Keep/x86_64/beta", "amd64", "beta")),
			repo("a/new", img("a/new", "app/org.example.New/x86_64/stable", "amd64")),
			store.RepositoryResult{Repository: "a/flaky", Err: errors.New("boom")},
		)
		time.Sleep(20 * time.Millisecond) // so the renewed lease end is visibly later
		must(t, s.WriteSyncBatch(ctx, r.ID, "o", results, store.SyncProgress{Done: 3, Total: 5}, time.Minute))
		want := []string{
			"a/flaky:app/org.example.Flaky/x86_64/stable",
			"a/gone:app/org.example.Gone/x86_64/stable",
			"a/keep:app/org.example.Keep/x86_64/beta",
			"a/new:app/org.example.New/x86_64/stable",
		}
		if got := repos(); !slices.Equal(got, want) {
			t.Errorf("after batch 1: %v, want %v", got, want)
		}
		if g := gen(); g != g0+1 {
			t.Errorf("generation after batch 1: %d, want %d", g, g0+1)
		}
		after, _ := s.GetRegistry(ctx, r.ID)
		if after.SyncRepositoriesDone != 3 || after.SyncRepositoriesTotal != 5 || after.SyncLeaseUntil <= before.SyncLeaseUntil || after.SyncState != store.SyncSyncing {
			t.Errorf("progress/lease after batch 1: before lease %d, after %+v", before.SyncLeaseUntil, after)
		}

		// A batch of failed repositories writes nothing and does not bump,
		// but still records progress.
		must(t, s.WriteSyncBatch(ctx, r.ID, "o", ok(store.RepositoryResult{Repository: "a/flaky", Err: errors.New("boom")}), store.SyncProgress{Done: 4, Total: 5}, time.Minute))
		if g := gen(); g != g0+1 {
			t.Errorf("generation after a failed-only batch: %d, want %d", g, g0+1)
		}
		if got, _ := s.GetRegistry(ctx, r.ID); got.SyncRepositoriesDone != 4 {
			t.Errorf("progress after failed-only batch: %+v", got)
		}

		// A repository the store rejects fails alone; the batch is written.
		bad := ok(
			repo("a/bad", &store.Image{Repository: "a/bad", Digest: "sha256:bad", Ref: "nonsense", OS: "linux", Architecture: "amd64", Tags: []string{"latest"}, Labels: map[string]string{}, IndexedAt: time.Now()}),
			repo("a/good", img("a/good", "app/org.example.Good/x86_64/stable", "amd64")),
		)
		must(t, s.WriteSyncBatch(ctx, r.ID, "o", bad, store.SyncProgress{Done: 5, Total: 5}, time.Minute))
		if bad[0].Err == nil || bad[1].Err != nil {
			t.Errorf("rejected repository not reported: %v / %v", bad[0].Err, bad[1].Err)
		}
		if !slices.Contains(repos(), "a/good:app/org.example.Good/x86_64/stable") {
			t.Error("good repository of a batch with a rejected one not written")
		}

		// Without the lease nothing is written, progress included.
		err = s.WriteSyncBatch(ctx, r.ID, "thief", ok(repo("a/stolen", img("a/stolen", "app/org.example.Stolen/x86_64/stable", "amd64"))), store.SyncProgress{Done: 9, Total: 9}, time.Minute)
		if !errors.Is(err, store.ErrLeaseLost) {
			t.Errorf("write without lease: %v", err)
		}
		if got, _ := s.GetRegistry(ctx, r.ID); got.SyncRepositoriesDone != 5 || slices.Contains(repos(), "a/stolen:app/org.example.Stolen/x86_64/stable") {
			t.Errorf("writer without lease changed data: %+v", got)
		}
		g2 := gen()

		// Stale removal: only repositories outside the present set go, one
		// bump; nothing to remove means no bump; the lease is checked.
		present := []string{"a/keep", "a/new", "a/flaky", "a/good", "a/bad", "a/never-indexed"}
		if _, err := s.RemoveStaleRepositories(ctx, r.ID, "thief", present); !errors.Is(err, store.ErrLeaseLost) {
			t.Errorf("removal without lease: %v", err)
		}
		if !slices.Contains(repos(), "a/gone:app/org.example.Gone/x86_64/stable") {
			t.Fatal("removal without lease removed images")
		}
		n, err := s.RemoveStaleRepositories(ctx, r.ID, "o", present)
		must(t, err)
		if n != 1 || slices.Contains(repos(), "a/gone:app/org.example.Gone/x86_64/stable") {
			t.Errorf("stale removal: n=%d repos=%v", n, repos())
		}
		if g := gen(); g != g2+1 {
			t.Errorf("generation after removal: %d, want %d", g, g2+1)
		}
		n, err = s.RemoveStaleRepositories(ctx, r.ID, "o", present)
		must(t, err)
		if n != 0 || gen() != g2+1 {
			t.Errorf("second removal: n=%d gen=%d", n, gen())
		}
		// Owner-less writes (seeding) work without a running sync.
		_, err = s.FinishSync(ctx, r.ID, "o", time.Now(), time.Second, nil)
		must(t, err)
		must(t, s.WriteSyncBatch(ctx, r.ID, "", ok(repo("a/seed", img("a/seed", "app/org.example.Seed/x86_64/stable", "amd64"))), store.SyncProgress{}, 0))
		if !slices.Contains(repos(), "a/seed:app/org.example.Seed/x86_64/stable") {
			t.Error("owner-less batch not written")
		}
	})
}
