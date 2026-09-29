package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/rs/zerolog"

	"github.com/lucarickli/flatpak-oci-notary/internal/flatpakindex"
	"github.com/lucarickli/flatpak-oci-notary/internal/store"
	"github.com/lucarickli/flatpak-oci-notary/internal/store/storetest"
)

// servedRepositories returns the OCI repositories the flatpak index of the
// repository slug serves right now, through the (generation-cached) handler.
func servedRepositories(t *testing.T, srv *httptest.Server, slug string) []string {
	t.Helper()
	res, err := srv.Client().Get(srv.URL + "/repo/" + slug + "/index/static")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var doc struct {
		Results []struct{ Name string }
	}
	if err := json.NewDecoder(res.Body).Decode(&doc); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("index: %v %d", err, res.StatusCode)
	}
	var out []string
	for _, r := range doc.Results {
		out = append(out, r.Name)
	}
	slices.Sort(out)
	return out
}

// fakeRegistry is an in-memory OCI registry whose requests for chosen
// repositories can be held (until released) or failed, to observe a sync
// in progress.
type fakeRegistry struct {
	t    *testing.T
	url  string
	host string

	mu       sync.Mutex
	blocked  map[string]chan struct{} // repository -> gate closed on release
	failing  map[string]bool
	requests map[string]int
}

func newFakeRegistry(t *testing.T) *fakeRegistry {
	t.Helper()
	f := &fakeRegistry{t: t, blocked: map[string]chan struct{}{}, failing: map[string]bool{}, requests: map[string]int{}}
	inner := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only tag listings are intercepted: /v2/<repo>/tags/list.
		if repo, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/v2/"), "/tags/list"); ok && r.URL.Path != "/v2/" {
			f.mu.Lock()
			f.requests[repo]++
			gate := f.blocked[repo]
			failing := f.failing[repo]
			f.mu.Unlock()
			if failing {
				http.Error(w, "upstream broken", http.StatusInternalServerError)
				return
			}
			if gate != nil {
				select {
				case <-gate:
				case <-r.Context().Done():
					return
				}
			}
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	f.url, f.host = srv.URL, strings.TrimPrefix(srv.URL, "http://")
	return f
}

// block holds tag listings of the repository until the returned function
// is called.
func (f *fakeRegistry) block(repo string) (release func()) {
	gate := make(chan struct{})
	f.mu.Lock()
	f.blocked[repo] = gate
	f.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(gate) }) }
}

func (f *fakeRegistry) fail(repo string, failing bool) {
	f.mu.Lock()
	f.failing[repo] = failing
	f.mu.Unlock()
}

// requested reports whether the repository's tags were listed.
func (f *fakeRegistry) requested(repo string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[repo] > 0
}

// push stores a flatpak image with the given ref under repo:tag.
func (f *fakeRegistry) push(repo, tag, ref string) {
	f.t.Helper()
	img := labelledImage(f.t, "amd64", map[string]string{
		LabelRef:               ref,
		"org.flatpak.metadata": "[Application]\nname=" + strings.Split(ref, "/")[1] + "\n",
	})
	r, err := name.ParseReference(f.host+"/"+repo+":"+tag, name.Insecure)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := remote.Write(r, img); err != nil {
		f.t.Fatal(err)
	}
}

func repositoriesOf(t *testing.T, st *store.Store, registryID string) []string {
	t.Helper()
	images, err := st.RegistryImages(context.Background(), registryID, false)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, img := range images {
		if !slices.Contains(out, img.Repository) {
			out = append(out, img.Repository)
		}
	}
	slices.Sort(out)
	return out
}

func generation(t *testing.T, st *store.Store) uint64 {
	t.Helper()
	g, err := st.Generation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// seed stores images of the given repositories as if an earlier sync found
// them.
func seed(t *testing.T, st *store.Store, registryID string, repos ...string) {
	t.Helper()
	var results []store.RepositoryResult
	for _, repo := range repos {
		ref := "app/org.example." + strings.ReplaceAll(strings.ReplaceAll(repo, "/", "."), "-", "") + "/x86_64/stable"
		results = append(results, store.RepositoryResult{Repository: repo, Images: []*store.Image{{
			Repository: repo, Digest: "sha256:" + strings.Repeat("0", 60) + "seed", MediaType: "application/vnd.oci.image.manifest.v1+json",
			OS: "linux", Architecture: "amd64", Tags: []string{"latest"}, Ref: ref, Labels: map[string]string{LabelRef: ref}, IndexedAt: time.Now(),
		}}})
	}
	if err := st.ReplaceRegistryImages(context.Background(), registryID, "", results); err != nil {
		t.Fatal(err)
	}
}

// TestIncrementalSync checks that repositories become visible while the
// sync runs (with progress and one generation bump per batch), and that
// images of repositories gone upstream are only removed at the end.
func TestIncrementalSync(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		ctx := context.Background()
		st := tg.Open(t)
		f := newFakeRegistry(t)
		f.push("apps/one", "latest", "app/org.example.One/x86_64/stable")
		f.push("apps/two", "latest", "app/org.example.Two/x86_64/stable")
		f.push("zz/last", "latest", "app/org.example.Last/x86_64/stable")
		release := f.block("zz/last")
		defer release()

		reg, err := st.CreateRegistry(ctx, &store.Registry{Name: "reg", URL: f.url, AuthType: store.AuthAnonymous, UseCatalog: true, SyncRequested: true})
		if err != nil {
			t.Fatal(err)
		}
		// From an earlier sync: a repository that vanished upstream.
		seed(t, st, reg.ID, "old/gone")
		g0 := generation(t, st)
		// A published repository serving everything, through the handler
		// with its generation-keyed cache.
		if _, err := st.CreateRepository(ctx, &store.Repository{Slug: "all", RegistryID: reg.ID, Sources: []store.Source{{RepositoryPattern: "*"}}}); err != nil {
			t.Fatal(err)
		}
		g0++ // the repository bumped it
		mux := http.NewServeMux()
		flatpakindex.NewHandler(st, zerolog.Nop(), func(*http.Request) string { return "https://notary.test" }, time.Minute).Register(mux)
		srv := httptest.NewServer(mux)
		defer srv.Close()
		if got := servedRepositories(t, srv, "all"); !slices.Equal(got, []string{"old/gone"}) {
			t.Fatalf("served before sync: %v", got)
		}

		x := New(st, zerolog.New(zerolog.NewTestWriter(t)), Options{Owner: "a", FlushRepositories: 1, FlushInterval: time.Hour})
		done := make(chan error, 1)
		go func() { _, err := x.SyncNow(ctx, reg.ID); done <- err }()

		state := func() *store.Registry {
			r, err := st.GetRegistry(ctx, reg.ID)
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		// The two fast repositories are stored (one batch each) while the
		// sync waits for the third.
		waitFor(t, "first repositories to be written", func() bool {
			return slices.Equal(repositoriesOf(t, st, reg.ID), []string{"apps/one", "apps/two", "old/gone"})
		})
		r := state()
		if r.SyncState != store.SyncSyncing || r.SyncRequested || r.SyncRepositoriesDone != 2 || r.SyncRepositoriesTotal != 3 {
			t.Errorf("state mid-sync: %s requested=%v progress=%d/%d", r.SyncState, r.SyncRequested, r.SyncRepositoriesDone, r.SyncRepositoriesTotal)
		}
		if g := generation(t, st); g != g0+2 {
			t.Errorf("generation after two single-repository batches: %d, want %d", g, g0+2)
		}
		if got := servedRepositories(t, srv, "all"); !slices.Equal(got, []string{"apps/one", "apps/two", "old/gone"}) {
			t.Errorf("served mid-sync: %v", got)
		}
		if !f.requested("zz/last") {
			waitFor(t, "last repository to be requested", func() bool { return f.requested("zz/last") })
		}

		release()
		if err := <-done; err != nil {
			t.Fatalf("sync: %v", err)
		}
		if got := repositoriesOf(t, st, reg.ID); !slices.Equal(got, []string{"apps/one", "apps/two", "zz/last"}) {
			t.Errorf("repositories after sync: %v", got)
		}
		r = state()
		if r.SyncState != store.SyncOK || r.SyncRepositoriesDone != 3 || r.SyncRepositoriesTotal != 3 || r.ImageCount != 3 {
			t.Errorf("state after sync: %+v", r)
		}
		// Third batch and the removal of the stale repository.
		if g := generation(t, st); g != g0+4 {
			t.Errorf("generation after sync: %d, want %d", g, g0+4)
		}
		if got := servedRepositories(t, srv, "all"); !slices.Equal(got, []string{"apps/one", "apps/two", "zz/last"}) {
			t.Errorf("served after sync: %v", got)
		}

		// A resync with everything already known: 3 repositories in one
		// batch, nothing stale, so exactly one bump.
		x2 := New(st, zerolog.New(zerolog.NewTestWriter(t)), Options{Owner: "a2", FlushRepositories: 25, FlushInterval: time.Hour})
		g1 := generation(t, st)
		if _, err := x2.SyncNow(ctx, reg.ID); err != nil {
			t.Fatal(err)
		}
		if g := generation(t, st); g != g1+1 {
			t.Errorf("generation after resync in one batch: %d, want %d", g, g1+1)
		}
	})
}

// TestFailedRepositoryKeepsImages checks that a repository that fails to
// index keeps its images, counts as done for the progress, fails the sync,
// and does not prevent the removal of repositories gone upstream.
func TestFailedRepositoryKeepsImages(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		ctx := context.Background()
		st := tg.Open(t)
		f := newFakeRegistry(t)
		f.push("apps/good", "latest", "app/org.example.Good/x86_64/stable")
		f.push("apps/bad", "latest", "app/org.example.Bad/x86_64/stable")
		reg, err := st.CreateRegistry(ctx, &store.Registry{Name: "reg", URL: f.url, AuthType: store.AuthAnonymous, UseCatalog: true})
		if err != nil {
			t.Fatal(err)
		}
		seed(t, st, reg.ID, "apps/bad", "old/gone")
		f.fail("apps/bad", true)

		x := New(st, zerolog.New(zerolog.NewTestWriter(t)), Options{Owner: "a"})
		ran, err := x.SyncNow(ctx, reg.ID)
		if !ran || err == nil || !strings.Contains(err.Error(), "1 of 2 repositories failed") {
			t.Fatalf("sync: ran=%v err=%v", ran, err)
		}
		if got := repositoriesOf(t, st, reg.ID); !slices.Equal(got, []string{"apps/bad", "apps/good"}) {
			t.Errorf("repositories: %v, want the failed one kept and the vanished one removed", got)
		}
		images, _ := st.RegistryImages(ctx, reg.ID, false)
		for _, img := range images {
			if img.Repository == "apps/bad" && !strings.HasSuffix(img.Digest, "seed") {
				t.Errorf("failed repository's images replaced: %+v", img)
			}
		}
		r, _ := st.GetRegistry(ctx, reg.ID)
		if r.SyncState != store.SyncError || !strings.Contains(r.LastSyncError, "apps/bad") || r.SyncRepositoriesDone != 2 || r.SyncRepositoriesTotal != 2 {
			t.Errorf("state: %+v", r)
		}

		// Once the repository works again its images are replaced.
		f.fail("apps/bad", false)
		if _, err := x.SyncNow(ctx, reg.ID); err != nil {
			t.Fatal(err)
		}
		images, _ = st.RegistryImages(ctx, reg.ID, false)
		for _, img := range images {
			if strings.HasSuffix(img.Digest, "seed") {
				t.Errorf("seeded image survived a successful sync: %+v", img)
			}
		}
	})
}

// TestAbortedSyncKeepsStale checks that an aborted sync keeps what it wrote
// but does not remove repositories it never got to compare with upstream,
// and that a failed discovery removes nothing either.
func TestAbortedSyncKeepsStale(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		ctx := context.Background()
		st := tg.Open(t)
		f := newFakeRegistry(t)
		f.push("apps/one", "latest", "app/org.example.One/x86_64/stable")
		f.push("zz/last", "latest", "app/org.example.Last/x86_64/stable")
		release := f.block("zz/last")
		defer release()
		reg, err := st.CreateRegistry(ctx, &store.Registry{Name: "reg", URL: f.url, AuthType: store.AuthAnonymous, UseCatalog: true})
		if err != nil {
			t.Fatal(err)
		}
		seed(t, st, reg.ID, "old/gone")

		x := New(st, zerolog.New(zerolog.NewTestWriter(t)), Options{Owner: "a", FlushRepositories: 1})
		sctx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { _, err := x.SyncNow(sctx, reg.ID); done <- err }()
		waitFor(t, "first repository to be written", func() bool {
			return slices.Contains(repositoriesOf(t, st, reg.ID), "apps/one")
		})
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("aborted sync error: %v", err)
		}
		if got := repositoriesOf(t, st, reg.ID); !slices.Equal(got, []string{"apps/one", "old/gone"}) {
			t.Errorf("repositories after abort: %v", got)
		}
		r, _ := st.GetRegistry(ctx, reg.ID)
		if r.SyncState != store.SyncError || r.LastSyncError != store.ShutdownInterruptedError || !r.SyncInterrupted() {
			t.Errorf("state after abort: %+v", r)
		}

		// A registry whose discovery fails keeps everything.
		reg.URL = "http://127.0.0.1:1" // nothing listens
		if _, err := st.UpdateRegistry(ctx, reg, false); err != nil {
			t.Fatal(err)
		}
		if _, err := x.SyncNow(ctx, reg.ID); err == nil {
			t.Fatal("sync against a dead registry succeeded")
		}
		if got := repositoriesOf(t, st, reg.ID); !slices.Equal(got, []string{"apps/one", "old/gone"}) {
			t.Errorf("repositories after failed discovery: %v", got)
		}
	})
}

// TestDue covers the scheduling rule.
func TestDue(t *testing.T) {
	now := time.Now()
	old, recent := now.Add(-2*time.Hour), now.Add(-time.Minute)
	for _, c := range []struct {
		name string
		reg  store.Registry
		want bool
	}{
		{"never synced", store.Registry{SyncIntervalMinutes: 60, SyncState: store.SyncNever}, true},
		{"interval elapsed", store.Registry{SyncIntervalMinutes: 60, SyncState: store.SyncOK, LastSyncAt: &old}, true},
		{"interval not elapsed", store.Registry{SyncIntervalMinutes: 60, SyncState: store.SyncOK, LastSyncAt: &recent}, false},
		{"manual only", store.Registry{SyncIntervalMinutes: 0, SyncState: store.SyncNever}, false},
		{"manual only, requested", store.Registry{SyncIntervalMinutes: 0, SyncRequested: true}, true},
		{"requested recently synced", store.Registry{SyncIntervalMinutes: 60, SyncState: store.SyncOK, LastSyncAt: &recent, SyncRequested: true}, true},
		{"syncing", store.Registry{SyncIntervalMinutes: 60, SyncState: store.SyncSyncing, SyncRequested: true}, false},
		{"interrupted", store.Registry{SyncIntervalMinutes: 60, SyncState: store.SyncError, LastSyncError: store.ShutdownInterruptedError, LastSyncAt: &recent}, true},
		{"failed recently", store.Registry{SyncIntervalMinutes: 60, SyncState: store.SyncError, LastSyncError: "boom", LastSyncAt: &recent}, false},
		// An interrupted sync is retried whatever the interval: for a
		// manual-only registry it was requested, and claiming consumed the request.
		{"manual only, shutdown", store.Registry{SyncIntervalMinutes: 0, SyncState: store.SyncError, LastSyncError: store.ShutdownInterruptedError}, true},
		{"manual only, lease expired", store.Registry{SyncIntervalMinutes: 0, SyncState: store.SyncError, LastSyncError: store.LeaseExpiredError, LastSyncAt: &recent}, true},
		{"manual only, restart", store.Registry{SyncIntervalMinutes: 0, SyncState: store.SyncError, LastSyncError: store.RestartInterruptedError}, true},
		{"manual only, failed", store.Registry{SyncIntervalMinutes: 0, SyncState: store.SyncError, LastSyncError: "boom", LastSyncAt: &recent}, false},
	} {
		if got := Due(&c.reg); got != c.want {
			t.Errorf("%s: due = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestSyncRequestScheduled checks that a requested sync of a manual-only
// registry is picked up by the scheduler and the request cleared.
func TestSyncRequestScheduled(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		ctx := context.Background()
		st := tg.Open(t)
		f := newFakeRegistry(t)
		f.push("apps/one", "latest", "app/org.example.One/x86_64/stable")
		reg, err := st.CreateRegistry(ctx, &store.Registry{Name: "reg", URL: f.url, AuthType: store.AuthAnonymous, UseCatalog: true, SyncIntervalMinutes: 0})
		if err != nil {
			t.Fatal(err)
		}
		x := New(st, zerolog.New(zerolog.NewTestWriter(t)), Options{Owner: "a", CheckInterval: 50 * time.Millisecond})
		stop := run(x)
		defer stop()
		time.Sleep(150 * time.Millisecond)
		if r, _ := st.GetRegistry(ctx, reg.ID); r.SyncState != store.SyncNever {
			t.Fatalf("manual-only registry synced without a request: %+v", r)
		}
		if err := st.RequestSync(ctx, reg.ID); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "requested sync", func() bool {
			r, _ := st.GetRegistry(ctx, reg.ID)
			return r.SyncState == store.SyncOK
		})
		r, _ := st.GetRegistry(ctx, reg.ID)
		if r.SyncRequested || r.ImageCount != 1 {
			t.Errorf("after requested sync: %+v", r)
		}
		if err := st.RequestSync(ctx, store.NewID()); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("request for an unknown registry: %v", err)
		}
	})
}

// TestInterruptedRequestRetried checks that a requested sync of a
// manual-only registry is not lost when the syncer that claimed it (which
// cleared the request) stops before finishing: another syncer retries it.
func TestInterruptedRequestRetried(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		ctx := context.Background()
		st := tg.Open(t)
		f := newFakeRegistry(t)
		f.push("apps/one", "latest", "app/org.example.One/x86_64/stable")
		release := f.block("apps/one")
		defer release()
		reg, err := st.CreateRegistry(ctx, &store.Registry{Name: "reg", URL: f.url, AuthType: store.AuthAnonymous,
			UseCatalog: true, SyncIntervalMinutes: 0, SyncRequested: true})
		if err != nil {
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
		opts := func(owner string) Options { return Options{Owner: owner, CheckInterval: 50 * time.Millisecond} }

		// Worker a picks up the request and is stopped mid-sync.
		stopA := run(New(st, log, opts("a")))
		waitFor(t, "sync on a to reach the registry", func() bool { return f.requested("apps/one") })
		stopA()
		if r := state(); r.SyncState != store.SyncError || r.LastSyncError != store.ShutdownInterruptedError || r.SyncRequested {
			t.Fatalf("after shutdown: %+v", r)
		}

		// Worker b retries it although the registry has no interval.
		release()
		stopB := run(New(tg.Open(t), log, opts("b")))
		defer stopB()
		waitFor(t, "retry on b", func() bool { return state().SyncState == store.SyncOK })
		if r := state(); r.ImageCount != 1 || r.SyncRequested {
			t.Errorf("after retry: %+v", r)
		}
	})
}

// TestSyncAll covers the one-shot run: selection by name, --due, skipping
// registries synced elsewhere and counting failures.
func TestSyncAll(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		ctx := context.Background()
		st := tg.Open(t)
		f := newFakeRegistry(t)
		f.push("apps/one", "latest", "app/org.example.One/x86_64/stable")
		create := func(name, url string, interval int) *store.Registry {
			r, err := st.CreateRegistry(ctx, &store.Registry{Name: name, URL: url, AuthType: store.AuthAnonymous, UseCatalog: true, SyncIntervalMinutes: interval})
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		good := create("good", f.url, 60)
		bad := create("bad", "http://127.0.0.1:1", 60)
		manual := create("manual", f.url, 0)
		busy := create("busy", f.url, 60)
		if ok, _ := st.ClaimSync(ctx, busy.ID, "elsewhere", time.Hour); !ok {
			t.Fatal("claim")
		}
		x := New(st, zerolog.New(zerolog.NewTestWriter(t)), Options{Owner: "a"})

		if _, err := x.SyncAll(ctx, Selection{Registries: []string{"good", "nope"}}); err == nil || !strings.Contains(err.Error(), `unknown registry "nope"`) {
			t.Errorf("unknown registry: %v", err)
		}

		// --due: good (never synced) and bad are due, manual is not, busy
		// is being synced.
		sum, err := x.SyncAll(ctx, Selection{Due: true})
		if err != nil {
			t.Fatal(err)
		}
		if sum != (Summary{Synced: 1, Failed: 1}) {
			t.Errorf("due run: %+v", sum)
		}
		if r, _ := st.GetRegistry(ctx, manual.ID); r.SyncState != store.SyncNever {
			t.Errorf("manual registry synced by --due: %+v", r)
		}
		if r, _ := st.GetRegistry(ctx, good.ID); r.SyncState != store.SyncOK {
			t.Errorf("good registry: %+v", r)
		}
		if r, _ := st.GetRegistry(ctx, bad.ID); r.SyncState != store.SyncError {
			t.Errorf("bad registry: %+v", r)
		}
		// Nothing due anymore, except on request.
		if sum, _ := x.SyncAll(ctx, Selection{Due: true}); sum != (Summary{}) {
			t.Errorf("second due run: %+v", sum)
		}
		if err := st.RequestSync(ctx, manual.ID); err != nil {
			t.Fatal(err)
		}
		if sum, _ := x.SyncAll(ctx, Selection{Due: true}); sum != (Summary{Synced: 1}) {
			t.Errorf("due run after request: %+v", sum)
		}
		if r, _ := st.GetRegistry(ctx, manual.ID); r.SyncState != store.SyncOK || r.SyncRequested {
			t.Errorf("manual registry after requested sync: %+v", r)
		}

		// Everything: the registry synced elsewhere is skipped, by id too.
		sum, err = x.SyncAll(ctx, Selection{})
		if err != nil {
			t.Fatal(err)
		}
		if sum != (Summary{Synced: 2, Failed: 1, Skipped: 1}) {
			t.Errorf("full run: %+v", sum)
		}
		if sum, _ := x.SyncAll(ctx, Selection{Registries: []string{busy.ID, "good", "good"}}); sum != (Summary{Synced: 1, Skipped: 1}) {
			t.Errorf("selected run: %+v", sum)
		}
		if r, _ := st.GetRegistry(ctx, busy.ID); r.SyncState != store.SyncSyncing || r.SyncOwner != "elsewhere" {
			t.Errorf("busy registry touched: %+v", r)
		}
	})
}

// TestResetIsInstanceScoped runs a "server" and a "syncer" instance on two
// Store instances sharing one database (also on SQLite): the server's
// startup reset must not touch the syncer's running sync, only its own.
func TestResetIsInstanceScoped(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		ctx := context.Background()
		serverStore, syncerStore := tg.Open(t), tg.Open(t)
		f := newFakeRegistry(t)
		f.push("apps/one", "latest", "app/org.example.One/x86_64/stable")
		release := f.block("apps/one")
		defer release()
		reg, err := serverStore.CreateRegistry(ctx, &store.Registry{Name: "reg", URL: f.url, AuthType: store.AuthAnonymous, UseCatalog: true})
		if err != nil {
			t.Fatal(err)
		}
		own, err := serverStore.CreateRegistry(ctx, &store.Registry{Name: "own", URL: f.url, AuthType: store.AuthAnonymous, UseCatalog: true})
		if err != nil {
			t.Fatal(err)
		}
		// A previous server process died mid-sync of "own".
		if ok, _ := serverStore.ClaimSync(ctx, own.ID, "host:8080", time.Hour); !ok {
			t.Fatal("claim")
		}

		log := zerolog.New(zerolog.NewTestWriter(t))
		syncer := New(syncerStore, log, Options{Owner: "host:sync-4242"})
		done := make(chan error, 1)
		go func() { _, err := syncer.SyncNow(ctx, reg.ID); done <- err }()
		waitFor(t, "syncer to hold the lease", func() bool {
			r, _ := serverStore.GetRegistry(ctx, reg.ID)
			return r.SyncState == store.SyncSyncing && r.SyncOwner == "host:sync-4242"
		})

		server := New(serverStore, log, Options{Owner: "host:8080"})
		server.ResetInterrupted(ctx)
		if r, _ := serverStore.GetRegistry(ctx, reg.ID); r.SyncState != store.SyncSyncing || r.SyncOwner != "host:sync-4242" {
			t.Errorf("server reset the syncer's live sync: %+v", r)
		}
		if r, _ := serverStore.GetRegistry(ctx, own.ID); r.SyncState != store.SyncError || r.LastSyncError != store.RestartInterruptedError {
			t.Errorf("server did not reset its own interrupted sync: %+v", r)
		}
		release()
		if err := <-done; err != nil {
			t.Errorf("syncer's sync failed: %v", err)
		}
	})
}
