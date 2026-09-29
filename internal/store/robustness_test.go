package store_test

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/lucarickli/flatpak-oci-notary/internal/store"
	"github.com/lucarickli/flatpak-oci-notary/internal/store/storetest"
)

// TestSearchNonASCII checks that queries with non-ASCII letters match their
// exact case and ASCII case variants on every dialect (both sides of the
// comparison are folded by the database).
func TestSearchNonASCII(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		s := tg.Open(t)
		r := newRegistry(t, s, "reg")
		u := img("apps/uebersicht", "app/org.example.Uebersicht/x86_64/stable", "amd64")
		u.Name = "Übersicht"
		c := img("apps/notepad", "app/org.example.Notepad/x86_64/stable", "amd64")
		c.Name = "Блокнот"
		must(t, s.ReplaceRegistryImages(ctx, r.ID, "", ok(repo("apps/uebersicht", u), repo("apps/notepad", c))))
		for _, q := range []string{"Über", "Übersicht", "bersicht", "ÜBERSICHT", "Блокнот", "локно"} {
			imgs, total, err := s.ListImages(ctx, store.ImageFilter{Query: q}, store.Page{})
			must(t, err)
			if total != 1 || len(imgs) != 1 {
				t.Errorf("ListImages %q: %d/%d, want 1", q, len(imgs), total)
			}
			repos, total, err := s.ListImageRepositories(ctx, store.ImageRepositoryFilter{Query: q}, store.Page{})
			must(t, err)
			if total != 1 || len(repos) != 1 {
				t.Errorf("ListImageRepositories %q: %d/%d, want 1", q, len(repos), total)
			}
		}
	})
}

// TestReplaceRegistryImagesBadValues checks that images the database could
// not store never block the rest of a registry sync.
func TestReplaceRegistryImagesBadValues(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		s := tg.Open(t)
		r := newRegistry(t, s, "reg")
		good := img("apps/good", "app/org.example.Good/x86_64/stable", "amd64")
		prev := img("apps/badkind", "app/org.example.Prev/x86_64/stable", "amd64")
		must(t, s.ReplaceRegistryImages(ctx, r.ID, "", ok(repo("apps/badkind", prev))))

		nul := img("apps/nul", "app/org.example.Nul/x86_64/stable", "amd64")
		nul.Version, nul.Name, nul.Summary = "1.0\x00beta", "N\x00ame", "caf\xe9"
		badKind := img("apps/badkind", "applicationextension/org.example.Bad/x86_64/stable", "amd64")
		results := ok(
			repo("apps/good", good),
			repo("apps/nul", nul),
			repo("apps/badkind", badKind),
			repo("apps/nulref", img("apps/nulref", "app/org.example.X\x00/x86_64/stable", "amd64")),
			repo("apps/longref", img("apps/longref", "app/org.example."+strings.Repeat("x", store.MaxRefLength)+"/x86_64/stable", "amd64")),
		)
		// A value only Postgres rejects (an incompressible indexed value
		// above the btree row size limit) exercises the per-repository
		// savepoint.
		var b [6000]byte
		_, _ = rand.Read(b[:])
		huge := img("apps/huge", "app/org.example.Huge/x86_64/stable", hex.EncodeToString(b[:]))
		// First, so the repositories after it are written after the failure.
		results = append(ok(repo("apps/huge", huge)), results...)

		must(t, s.ReplaceRegistryImages(ctx, r.ID, "", results))
		failed := map[string]bool{}
		for _, res := range results {
			if res.Err != nil {
				failed[res.Repository] = true
			}
		}
		for _, repo := range []string{"apps/badkind", "apps/nulref", "apps/longref"} {
			if !failed[repo] {
				t.Errorf("%s: invalid ref accepted", repo)
			}
		}
		if failed["apps/good"] || failed["apps/nul"] {
			t.Errorf("valid repositories failed: %v", failed)
		}
		if wantHuge := tg.Name == store.DriverPostgres; failed["apps/huge"] != wantHuge {
			t.Errorf("huge value failed = %v, want %v: %v", failed["apps/huge"], wantHuge, results[0].Err)
		}

		all, err := s.RegistryImages(ctx, r.ID, false)
		must(t, err)
		byRepo := map[string]*store.Image{}
		for _, im := range all {
			byRepo[im.Repository] = im
		}
		if byRepo["apps/good"] == nil {
			t.Error("good repository not stored next to bad ones")
		}
		if n := byRepo["apps/nul"]; n == nil || n.Version != "1.0beta" || n.Name != "Name" || !utf8.ValidString(n.Summary) {
			t.Errorf("text with NUL/invalid UTF-8 not cleaned: %+v", n)
		}
		// A repository that failed keeps its previous images.
		if p := byRepo["apps/badkind"]; p == nil || p.Ref != prev.Ref {
			t.Errorf("failed repository lost its previous images: %+v", p)
		}
	})
}

func TestFinishSyncUnstorableError(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		s := tg.Open(t)
		r := newRegistry(t, s, "reg")
		for _, msg := range []string{"unexpected status code 502 Bad Gateway: caf\xe9 \x00", strings.Repeat("é", 10000)} {
			claimed, err := s.ClaimSync(ctx, r.ID, "o", time.Minute)
			must(t, err)
			if !claimed {
				t.Fatal("claim failed")
			}
			done, err := s.FinishSync(ctx, r.ID, "o", time.Now(), time.Second, errors.New(msg))
			must(t, err)
			got, _ := s.GetRegistry(ctx, r.ID)
			if !done || got.SyncState != store.SyncError || got.SyncOwner != "" || got.LastSyncAt == nil ||
				got.LastSyncError == "" || !utf8.ValidString(got.LastSyncError) || strings.ContainsRune(got.LastSyncError, 0) ||
				len(got.LastSyncError) > 4<<10+len("…") {
				t.Errorf("finish with %.40q: done=%v %+v", msg, done, got)
			}
		}
	})
}

func TestInterruptSync(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		a, b := tg.Open(t), tg.Open(t)
		r1, r2, r3 := newRegistry(t, a, "one"), newRegistry(t, a, "two"), newRegistry(t, a, "three")

		// A sync that ran before stays recorded when the next one is interrupted.
		_, _ = a.ClaimSync(ctx, r1.ID, "a", time.Minute)
		_, _ = a.FinishSync(ctx, r1.ID, "a", time.Now().Add(-time.Hour), time.Second, nil)
		before, _ := a.GetRegistry(ctx, r1.ID)
		_, _ = a.ClaimSync(ctx, r1.ID, "a", time.Minute)
		if done, _ := b.InterruptSync(ctx, r1.ID, "b"); done {
			t.Error("non-owner interrupted the sync")
		}
		done, err := a.InterruptSync(ctx, r1.ID, "a")
		must(t, err)
		got, _ := b.GetRegistry(ctx, r1.ID)
		if !done || got.SyncState != store.SyncError || got.LastSyncError != store.ShutdownInterruptedError || got.SyncOwner != "" ||
			!got.SyncInterrupted() || got.LastSyncAt == nil || !got.LastSyncAt.Equal(*before.LastSyncAt) ||
			got.LastSyncDurationMs != before.LastSyncDurationMs {
			t.Errorf("after interrupt: %+v", got)
		}
		if claimed, _ := b.ClaimSync(ctx, r1.ID, "b", time.Minute); !claimed {
			t.Error("interrupted sync not claimable")
		}

		// Restart: only the given owner's syncs, never everything (another
		// process may share the database, also on SQLite).
		_, _ = a.ClaimSync(ctx, r2.ID, "a", time.Minute)
		_, _ = a.ClaimSync(ctx, r3.ID, "c", time.Minute)
		n, err := a.ResetInterruptedSyncs(ctx, "a")
		must(t, err)
		if got, _ := a.GetRegistry(ctx, r2.ID); n != 1 || got.LastSyncError != store.RestartInterruptedError || !got.SyncInterrupted() {
			t.Errorf("reset own syncs: %d, %+v", n, got)
		}
		if got, _ := a.GetRegistry(ctx, r3.ID); got.SyncState != store.SyncSyncing {
			t.Errorf("another owner's sync was reset: %+v", got)
		}
		if _, err := a.ResetInterruptedSyncs(ctx, ""); err == nil {
			t.Error("reset without an owner accepted")
		}
		if got, _ := a.GetRegistry(ctx, r3.ID); got.SyncState != store.SyncSyncing || got.SyncOwner != "c" {
			t.Errorf("after refused reset: %+v", got)
		}
		fresh, _ := a.GetRegistry(ctx, r2.ID)
		fresh.SyncState, fresh.LastSyncError = store.SyncError, "list catalog: boom"
		if fresh.SyncInterrupted() {
			t.Error("an ordinary failure reported as interrupted")
		}
	})
}

// TestOrderByteWise checks that listings are ordered by byte value on every
// dialect, independent of the database collation.
func TestOrderByteWise(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		s := tg.Open(t)
		r := newRegistry(t, s, "reg")
		must(t, s.ReplaceRegistryImages(ctx, r.ID, "", ok(
			repo("apps/b", img("apps/b", "app/org.b.x/x86_64/stable", "amd64")),
			repo("Apps/B", img("Apps/B", "app/org.B.y/x86_64/stable", "amd64")),
			repo("apps/a_z", img("apps/a_z", "app/org.a_z/x86_64/stable", "amd64")),
			repo("apps/aa", img("apps/aa", "app/org.aa/x86_64/stable", "amd64")),
		)))
		imgs, _, err := s.ListImages(ctx, store.ImageFilter{}, store.Page{})
		must(t, err)
		var got []string
		for _, im := range imgs {
			got = append(got, im.Ref)
		}
		want := []string{"app/org.B.y/x86_64/stable", "app/org.a_z/x86_64/stable", "app/org.aa/x86_64/stable", "app/org.b.x/x86_64/stable"}
		if !slices.Equal(got, want) {
			t.Errorf("image order %v, want %v", got, want)
		}
		repos, _, err := s.ListImageRepositories(ctx, store.ImageRepositoryFilter{}, store.Page{})
		must(t, err)
		got = nil
		for _, r := range repos {
			got = append(got, r.Repository)
		}
		if want := []string{"Apps/B", "apps/a_z", "apps/aa", "apps/b"}; !slices.Equal(got, want) {
			t.Errorf("repository order %v, want %v", got, want)
		}
	})
}

// TestGenerationReadRacingOwnWrite forces a generation read that fetched the
// old value to finish after a local write stored the new one: the cache
// must keep the newer value.
func TestGenerationReadRacingOwnWrite(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		cfg := tg.Config
		cfg.GenerationCacheTTL = time.Hour
		cfg.Logger = zerolog.Nop()
		s, err := store.Open(ctx, cfg)
		must(t, err)
		defer s.Close()
		r := newRegistry(t, s, "reg")

		var armed atomic.Bool
		reached, release := make(chan struct{}), make(chan struct{})
		must(t, s.DB().Callback().Query().After("gorm:query").Register("test:block_generation", func(db *gorm.DB) {
			if db.Statement.Table == "index_state" && armed.CompareAndSwap(true, false) {
				close(reached)
				<-release
			}
		}))
		armed.Store(true)
		read := make(chan uint64, 1)
		go func() {
			g, _ := s.Generation(ctx)
			read <- g
		}()
		<-reached
		must(t, s.ReplaceRegistryImages(ctx, r.ID, "", ok(repo("a/x", img("a/x", "app/org.x.A/x86_64/stable", "amd64")))))
		written, _ := s.LastGeneration()
		close(release)
		<-read
		g, err := s.Generation(ctx)
		must(t, err)
		if g != written {
			t.Errorf("generation after a racing read = %d, want own write's %d", g, written)
		}
	})
}
