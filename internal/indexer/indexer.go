// Package indexer discovers flatpak images on upstream OCI registries and
// records them in the store.
package indexer

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/lucarickli/flatpak-oci-notary/internal/glob"
	"github.com/lucarickli/flatpak-oci-notary/internal/store"
)

// LabelRef is the label flatpak uses to identify flatpak images.
const LabelRef = "org.flatpak.ref"

// Options configure the indexer.
type Options struct {
	// Concurrency is the number of OCI repositories indexed in parallel per registry.
	Concurrency int
	// SyncTimeout bounds a single registry sync.
	SyncTimeout time.Duration
	// CheckInterval is how often the scheduler looks for due registries.
	CheckInterval time.Duration
	// Owner identifies this instance in sync leases; random when empty. A
	// stable owner (unique per process sharing the database) lets a
	// restarted instance release the syncs it left running at once instead
	// of waiting for their lease to expire.
	Owner string
	// LeaseDuration is how long a sync lease lasts without renewal. A
	// replica that dies mid-sync frees its registries after this long.
	LeaseDuration time.Duration
	// HeartbeatInterval is how often a running sync renews its lease.
	HeartbeatInterval time.Duration
	// FlushInterval and FlushRepositories bound how long indexed
	// repositories wait before they are written: a batch is written once it
	// holds FlushRepositories repositories or FlushInterval after its first
	// one, whichever comes first. Defaults: 5s and 25.
	FlushInterval     time.Duration
	FlushRepositories int
}

// Indexer syncs registries, periodically and on demand. Several instances
// may share one database: a sync is claimed through a lease in the store, so
// only one instance syncs a given registry at a time.
type Indexer struct {
	store *store.Store
	log   zerolog.Logger
	opts  Options
	owner string

	mu      sync.Mutex
	running map[string]bool
	// notFlatpak caches manifest digests known not to be flatpaks, per registry.
	notFlatpak map[string]map[string]bool
	wg         sync.WaitGroup

	// ctx is the parent of all syncs; cancelled when Run stops.
	ctx    context.Context
	cancel context.CancelFunc
}

func New(s *store.Store, log zerolog.Logger, opts Options) *Indexer {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}
	if opts.SyncTimeout <= 0 {
		opts.SyncTimeout = 30 * time.Minute
	}
	if opts.CheckInterval <= 0 {
		opts.CheckInterval = 30 * time.Second
	}
	if opts.LeaseDuration <= 0 {
		opts.LeaseDuration = 2 * time.Minute
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = opts.LeaseDuration / 4
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = 5 * time.Second
	}
	if opts.FlushRepositories <= 0 {
		opts.FlushRepositories = 25
	}
	if opts.Owner == "" {
		opts.Owner = rand.Text()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Indexer{
		ctx:        ctx,
		cancel:     cancel,
		store:      s,
		log:        log.With().Str("component", "indexer").Str("instance", opts.Owner).Logger(),
		opts:       opts,
		owner:      opts.Owner,
		running:    map[string]bool{},
		notFlatpak: map[string]map[string]bool{},
	}
}

// Owner is this instance's id in sync leases.
func (x *Indexer) Owner() string { return x.owner }

// Run schedules syncs of due registries until ctx is cancelled, then waits
// for running syncs to stop. It may run on every replica (and next to a
// standalone syncer): claims are atomic.
func (x *Indexer) Run(ctx context.Context) {
	defer x.wg.Wait()
	defer x.cancel()
	ticker := time.NewTicker(x.opts.CheckInterval)
	defer ticker.Stop()
	for {
		x.scheduleDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ResetInterrupted marks syncs that a previous process of this instance
// (same Options.Owner) left running, because it crashed or was killed, as
// interrupted, so they are retried right away instead of showing "syncing"
// until their lease expires. Syncs held by other instances are never
// touched: a server and a standalone syncer may share one database, on
// Postgres and on SQLite alike, and a dead instance's lease expires on its
// own. Call it once at startup, before Run and before anything can trigger
// a sync.
func (x *Indexer) ResetInterrupted(ctx context.Context) {
	n, err := x.store.ResetInterruptedSyncs(ctx, x.owner)
	switch {
	case err != nil:
		x.log.Error().Err(err).Msg("reset interrupted syncs")
	case n > 0:
		x.log.Warn().Int64("registries", n).Msg("reset syncs interrupted by a restart")
	}
}

func (x *Indexer) scheduleDue(ctx context.Context) {
	regs, err := x.store.ListRegistries(ctx)
	if err != nil {
		if ctx.Err() == nil {
			x.log.Error().Err(err).Msg("list registries")
		}
		return
	}
	for _, r := range regs {
		if Due(r) {
			x.Trigger(ctx, r.ID)
		}
	}
}

// Due reports whether a registry should be synced now: a sync was requested
// (SyncRegistry, or the registry was created or changed), its interval
// elapsed since the last completed sync, or its last sync stopped before it
// finished (shutdown, restart, expired lease). An interrupted sync is retried
// right away whatever the interval, also for a registry with a zero interval:
// it was wanted (by its schedule or by a request that claiming the sync
// consumed). Otherwise registries with a zero interval are only synced on
// request, and a registry being synced is never due.
func Due(r *store.Registry) bool {
	if r.SyncState == store.SyncSyncing {
		return false
	}
	if r.SyncRequested || r.SyncInterrupted() {
		return true
	}
	if r.SyncIntervalMinutes <= 0 {
		return false
	}
	return r.LastSyncAt == nil || time.Since(*r.LastSyncAt) >= time.Duration(r.SyncIntervalMinutes)*time.Minute
}

// claim takes the registry's sync lease for this instance. It returns a
// release function and whether the lease was taken; false without an error
// means a sync is already running here or on another instance.
func (x *Indexer) claim(ctx context.Context, registryID string) (release func(), claimed bool, err error) {
	x.mu.Lock()
	if x.running[registryID] {
		x.mu.Unlock()
		return nil, false, nil
	}
	x.running[registryID] = true
	x.mu.Unlock()
	release = func() {
		x.mu.Lock()
		delete(x.running, registryID)
		x.mu.Unlock()
	}
	claimed, err = x.store.ClaimSync(ctx, registryID, x.owner, x.opts.LeaseDuration)
	if err != nil || !claimed {
		release()
		return nil, false, err
	}
	return release, true, nil
}

// Trigger starts a background sync of the registry unless one is running
// (here or on another instance). It reports whether a new sync was started.
func (x *Indexer) Trigger(ctx context.Context, registryID string) bool {
	release, ok, err := x.claim(ctx, registryID)
	if err != nil {
		x.log.Error().Err(err).Str("registry_id", registryID).Msg("claim sync lease")
	}
	if !ok {
		return false
	}
	x.wg.Add(1)
	go func() {
		defer x.wg.Done()
		defer release()
		// Syncs outlive the triggering request but stop on shutdown.
		sctx, cancel := context.WithTimeout(x.ctx, x.opts.SyncTimeout)
		defer cancel()
		_ = x.runSync(sctx, registryID)
	}()
	return true
}

// SyncNow syncs a registry synchronously, bounded by SyncTimeout. It
// reports whether a sync ran (false without an error when one is already
// running here or on another instance) and how it ended: nil on success,
// the sync error otherwise (also when the lease was lost or ctx was
// cancelled).
func (x *Indexer) SyncNow(ctx context.Context, registryID string) (ran bool, err error) {
	release, ok, err := x.claim(ctx, registryID)
	if err != nil {
		return false, fmt.Errorf("claim sync lease: %w", err)
	}
	if !ok {
		return false, nil
	}
	defer release()
	sctx, cancel := context.WithTimeout(ctx, x.opts.SyncTimeout)
	defer cancel()
	return true, x.runSync(sctx, registryID)
}

// Selection picks the registries SyncAll syncs.
type Selection struct {
	// Registries are names or ids; empty selects all registries.
	Registries []string
	// Due restricts the selection to registries that are due (see Due).
	Due bool
}

// Summary counts the outcomes of SyncAll.
type Summary struct {
	// Synced registries completed without error, Failed ones ended in an
	// error, Skipped ones were being synced by another instance.
	Synced, Failed, Skipped int
}

// SyncAll syncs the selected registries one after another, for the one-shot
// "notary sync". Registries that are not due (with Selection.Due) are left
// out; registries being synced by another instance are skipped and
// counted, not failed. The error is only set when the selection is invalid
// (an unknown registry), the registries cannot be listed or ctx ends; sync
// failures are counted in the summary and logged per registry.
func (x *Indexer) SyncAll(ctx context.Context, sel Selection) (Summary, error) {
	var sum Summary
	regs, err := x.store.ListRegistries(ctx)
	if err != nil {
		return sum, fmt.Errorf("list registries: %w", err)
	}
	selected, err := selectRegistries(regs, sel.Registries)
	if err != nil {
		return sum, err
	}
	for _, r := range selected {
		log := x.log.With().Str("registry_id", r.ID).Str("registry", r.Name).Logger()
		if sel.Due && !Due(r) {
			log.Debug().Msg("registry not due; skipped")
			continue
		}
		ran, err := x.SyncNow(ctx, r.ID)
		switch {
		case ctx.Err() != nil:
			return sum, fmt.Errorf("sync interrupted: %w", context.Cause(ctx))
		case err != nil:
			sum.Failed++
			if !ran {
				// Claiming the lease failed (a database error), so sync,
				// which logs its own failures, never ran.
				log.Error().Err(err).Msg("sync failed")
			}
		case !ran:
			sum.Skipped++
			log.Info().Msg("sync skipped: another instance is syncing this registry")
		default:
			sum.Synced++
		}
	}
	return sum, nil
}

// selectRegistries resolves names or ids to registries, keeping the listing
// order; empty selects all.
func selectRegistries(regs []*store.Registry, names []string) ([]*store.Registry, error) {
	if len(names) == 0 {
		return regs, nil
	}
	var out []*store.Registry
	seen := map[string]bool{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		var found *store.Registry
		for _, r := range regs {
			if r.Name == n || r.ID == n {
				found = r
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("unknown registry %q", n)
		}
		if !seen[found.ID] {
			seen[found.ID] = true
			out = append(out, found)
		}
	}
	return out, nil
}

// runSync runs a claimed sync while renewing its lease. Losing the lease
// (another instance took over after this one failed to renew in time)
// aborts the sync.
func (x *Indexer) runSync(ctx context.Context, registryID string) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(x.opts.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			renewed, err := x.store.RenewSyncLease(ctx, registryID, x.owner, x.opts.LeaseDuration)
			switch {
			case err != nil:
				// Transient database trouble: keep going and retry next tick.
				if ctx.Err() == nil {
					x.log.Warn().Err(err).Str("registry_id", registryID).Msg("renew sync lease")
				}
			case !renewed:
				cancel(store.ErrLeaseLost)
				return
			}
		}
	}()
	return x.sync(ctx, registryID)
}

// Forget drops cached state for a deleted registry.
func (x *Indexer) Forget(registryID string) {
	x.mu.Lock()
	delete(x.notFlatpak, registryID)
	x.mu.Unlock()
}

// sync runs a claimed sync to its end, records the outcome and returns it.
func (x *Indexer) sync(ctx context.Context, registryID string) error {
	start := time.Now()
	reg, err := x.store.GetRegistry(ctx, registryID)
	if err != nil {
		log := x.log.With().Str("registry_id", registryID).Logger()
		if x.interrupted(ctx, registryID, log, time.Since(start)) {
			return context.Cause(ctx)
		}
		err = fmt.Errorf("load registry: %w", err)
		log.Error().Err(err).Msg("sync failed")
		// Release the lease rather than leaving the registry "syncing".
		_, _ = x.store.FinishSync(context.WithoutCancel(ctx), registryID, x.owner, time.Now(), time.Since(start), err)
		return err
	}
	log := x.log.With().Str("registry_id", reg.ID).Str("registry", reg.Name).Logger()
	log.Info().Msg("sync started")

	stats, syncErr := x.syncRegistry(ctx, reg, log)
	d := time.Since(start)
	if errors.Is(context.Cause(ctx), store.ErrLeaseLost) || errors.Is(syncErr, store.ErrLeaseLost) {
		log.Warn().Dur("duration", d).Msg("sync lease lost to another instance; sync stopped")
		return store.ErrLeaseLost
	}
	if syncErr != nil && x.interrupted(ctx, reg.ID, log, d) {
		return context.Cause(ctx)
	}
	recorded, err := x.store.FinishSync(context.WithoutCancel(ctx), reg.ID, x.owner, time.Now(), d, syncErr)
	if err != nil {
		log.Error().Err(err).Msg("record sync result")
		if syncErr == nil {
			syncErr = fmt.Errorf("record sync result: %w", err)
		}
	} else if !recorded {
		log.Warn().Dur("duration", d).Msg("sync lease lost to another instance; result discarded")
		return store.ErrLeaseLost
	}
	ev := log.Info()
	if syncErr != nil {
		ev = log.Warn().Err(syncErr)
	}
	ev.Dur("duration", d).
		Int("repositories", stats.repositories).
		Int("failed_repositories", stats.failed).
		Int("removed_repositories", stats.removed).
		Int("images", stats.images).
		Int("fetched_manifests", stats.fetched).
		Int("batches", stats.batches).
		Msg("sync finished")
	return syncErr
}

// interrupted handles a sync stopped by shutdown (ctx cancelled, not timed
// out by SyncTimeout, whose cause is DeadlineExceeded): the lease is
// released without recording an attempt, so another replica, or this one
// after its restart, retries the sync. It reports whether that was the case.
func (x *Indexer) interrupted(ctx context.Context, registryID string, log zerolog.Logger, d time.Duration) bool {
	if ctx.Err() == nil || !errors.Is(context.Cause(ctx), context.Canceled) {
		return false
	}
	released, err := x.store.InterruptSync(context.WithoutCancel(ctx), registryID, x.owner)
	switch {
	case err != nil:
		log.Error().Err(err).Msg("release interrupted sync")
	case released:
		log.Warn().Dur("duration", d).Msg("sync interrupted by shutdown; lease released")
	}
	return true
}

type syncStats struct {
	repositories, failed, removed, images, fetched, batches int
}

// syncRegistry indexes every repository of the registry, writing results in
// batches while it runs (see batchWriter), then drops the images of
// repositories that are gone upstream.
func (x *Indexer) syncRegistry(ctx context.Context, reg *store.Registry, log zerolog.Logger) (syncStats, error) {
	var stats syncStats
	c, err := newClient(ctx, reg)
	if err != nil {
		return stats, err
	}
	puller, err := remote.NewPuller(c.options...)
	if err != nil {
		return stats, err
	}

	repos, err := x.discover(ctx, c, puller, reg)
	if err != nil {
		return stats, err
	}
	stats.repositories = len(repos)
	if _, err := x.store.SetSyncTotal(ctx, reg.ID, x.owner, len(repos)); err != nil && ctx.Err() == nil {
		// Progress only; a lost lease is noticed by the heartbeat and the
		// next batch.
		log.Warn().Err(err).Msg("record sync total")
	}

	existing, err := x.store.RegistryImages(ctx, reg.ID, true)
	if err != nil {
		return stats, err
	}
	known := map[string][]*store.Image{} // key: repository + "@" + (list) digest
	for _, img := range existing {
		known[img.Repository+"@"+img.Digest] = append(known[img.Repository+"@"+img.Digest], img)
		if img.ListDigest != "" {
			known[img.Repository+"@"+img.ListDigest] = append(known[img.Repository+"@"+img.ListDigest], img)
		}
	}

	// Workers index repositories and hand the results to the writer, which
	// stores them in batches. A failed write cancels the workers (pctx);
	// they never block on a writer that stopped reading.
	pctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	g, gctx := errgroup.WithContext(pctx)
	g.SetLimit(x.opts.Concurrency)
	results := make(chan store.RepositoryResult)
	w := &batchWriter{x: x, registryID: reg.ID, log: log, total: len(repos)}
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		// The writer lives on pctx, not gctx: the group's context ends
		// when Wait returns, before the last results are flushed.
		if err := w.run(pctx, results); err != nil {
			cancel(err)
		}
	}()
	var mu sync.Mutex
	for _, repo := range repos {
		g.Go(func() error {
			imgs, fetched, err := x.indexRepository(gctx, c, puller, reg, repo, known)
			if err != nil && gctx.Err() == nil {
				log.Debug().Err(err).Str("repository", repo).Msg("index repository failed")
			}
			mu.Lock()
			stats.fetched += fetched
			mu.Unlock()
			select {
			case results <- store.RepositoryResult{Repository: repo, Images: imgs, Err: err}:
			case <-gctx.Done():
			}
			return nil
		})
	}
	_ = g.Wait()
	close(results)
	<-writerDone
	stats.failed, stats.images, stats.batches = w.failed, w.images, w.batches
	if err := ctx.Err(); err != nil {
		return stats, fmt.Errorf("sync aborted: %w", context.Cause(ctx))
	}
	if w.err != nil {
		return stats, fmt.Errorf("store images: %w", w.err)
	}

	// Every repository upstream has been seen (indexed or failed): images of
	// repositories that are gone can go too.
	removed, err := x.store.RemoveStaleRepositories(ctx, reg.ID, x.owner, repos)
	if err != nil {
		return stats, fmt.Errorf("remove stale repositories: %w", err)
	}
	stats.removed = removed
	if w.failed > 0 {
		return stats, fmt.Errorf("%d of %d repositories failed, first error: %w", w.failed, len(repos), w.firstErr)
	}
	return stats, nil
}

// batchWriter stores repository results as they arrive, in batches bounded
// by Options.FlushInterval and Options.FlushRepositories, so a long sync
// shows progress and its images become visible early. Every flush renews
// the lease and records the progress.
type batchWriter struct {
	x          *Indexer
	registryID string
	log        zerolog.Logger
	total      int

	batch []store.RepositoryResult
	// done counts the repositories written (or failed) so far.
	done, failed, images, batches int
	firstErr                      error
	// err is the error that stopped the writer, if any.
	err error
}

func (w *batchWriter) run(ctx context.Context, results <-chan store.RepositoryResult) error {
	timer := time.NewTimer(w.x.opts.FlushInterval)
	timer.Stop()
	defer timer.Stop()
	fail := func(err error) error {
		w.err = err
		return err
	}
	for {
		select {
		case r, ok := <-results:
			if !ok {
				if err := w.flush(ctx); err != nil {
					return fail(err)
				}
				return nil
			}
			if len(w.batch) == 0 {
				timer.Reset(w.x.opts.FlushInterval)
			}
			w.batch = append(w.batch, r)
			if len(w.batch) >= w.x.opts.FlushRepositories {
				if err := w.flush(ctx); err != nil {
					return fail(err)
				}
				timer.Stop()
			}
		case <-timer.C:
			if err := w.flush(ctx); err != nil {
				return fail(err)
			}
		case <-ctx.Done():
			return fail(ctx.Err())
		}
	}
}

func (w *batchWriter) flush(ctx context.Context) error {
	if len(w.batch) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	batch := w.batch
	w.batch = nil
	w.done += len(batch)
	err := w.x.store.WriteSyncBatch(ctx, w.registryID, w.x.owner, batch,
		store.SyncProgress{Done: w.done, Total: w.total}, w.x.opts.LeaseDuration)
	if err != nil {
		return err
	}
	// Repositories the store rejected have their Err set now.
	for _, r := range batch {
		if r.Err != nil {
			w.failed++
			if w.firstErr == nil {
				w.firstErr = fmt.Errorf("%s: %w", r.Repository, r.Err)
			}
			continue
		}
		w.images += len(r.Images)
	}
	w.batches++
	w.log.Info().Int("done", w.done).Int("total", w.total).Int("images", w.images).Int("failed_repositories", w.failed).
		Msg("sync progress")
	return nil
}

// discover returns the OCI repositories to index.
func (x *Indexer) discover(ctx context.Context, c *client, puller *remote.Puller, reg *store.Registry) ([]string, error) {
	set := map[string]bool{}
	for _, r := range reg.Repositories {
		if r = strings.Trim(strings.TrimSpace(r), "/"); r != "" {
			set[r] = true
		}
	}
	if reg.UseCatalog {
		catalog, err := puller.Catalog(ctx, c.registry)
		if err != nil {
			return nil, fmt.Errorf("list catalog: %w", err)
		}
		for _, r := range catalog {
			if glob.MatchAny(reg.RepositoryPatterns, r) {
				set[r] = true
			}
		}
	}
	if len(set) == 0 {
		return nil, errors.New("no repositories to index: enable catalog discovery or list repositories explicitly")
	}
	out := make([]string, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Strings(out)
	return out, nil
}

// indexRepository returns all flatpak images of one OCI repository.
func (x *Indexer) indexRepository(ctx context.Context, c *client, puller *remote.Puller, reg *store.Registry,
	repoName string, known map[string][]*store.Image) ([]*store.Image, int, error) {
	repo := c.repo(repoName)
	tags, err := puller.List(ctx, repo)
	if err != nil {
		return nil, 0, fmt.Errorf("list tags: %w", err)
	}

	// Resolve tags to manifest digests.
	type target struct {
		mediaType types.MediaType
		tags      []string
	}
	targets := map[string]*target{}
	var order []string
	for _, tag := range tags {
		if !glob.MatchAny(reg.TagPatterns, tag) {
			continue
		}
		desc, err := puller.Head(ctx, repo.Tag(tag))
		if err != nil {
			return nil, 0, fmt.Errorf("resolve tag %s: %w", tag, err)
		}
		d := desc.Digest.String()
		t, ok := targets[d]
		if !ok {
			t = &target{mediaType: desc.MediaType}
			targets[d] = t
			order = append(order, d)
		}
		t.tags = append(t.tags, tag)
	}

	byDigest := map[string]*store.Image{}
	var images []*store.Image
	add := func(img *store.Image, tags []string) {
		if prev, ok := byDigest[img.Digest]; ok {
			prev.Tags = mergeTags(prev.Tags, tags)
			return
		}
		cp := *img
		cp.Tags = mergeTags(nil, tags)
		byDigest[img.Digest] = &cp
		images = append(images, &cp)
	}

	fetched := 0
	for _, d := range order {
		t := targets[d]
		if prev, ok := known[repoName+"@"+d]; ok {
			for _, img := range prev {
				add(img, t.tags)
			}
			continue
		}
		if x.isNotFlatpak(reg.ID, d) {
			continue
		}
		found, err := x.fetchImages(ctx, puller, repo, d)
		if err != nil {
			return nil, fetched, fmt.Errorf("fetch %s (%s): %w", d, strings.Join(t.tags, ","), err)
		}
		fetched++
		if len(found) == 0 {
			x.markNotFlatpak(reg.ID, d)
			continue
		}
		for _, img := range found {
			add(img, t.tags)
		}
	}
	return images, fetched, nil
}

// fetchImages loads a manifest (or image index) and returns the flatpak images in it.
func (x *Indexer) fetchImages(ctx context.Context, puller *remote.Puller, repo name.Repository, digest string) ([]*store.Image, error) {
	desc, err := puller.Get(ctx, repo.Digest(digest))
	if err != nil {
		return nil, err
	}
	if desc.MediaType.IsIndex() {
		idx, err := desc.ImageIndex()
		if err != nil {
			return nil, err
		}
		im, err := idx.IndexManifest()
		if err != nil {
			return nil, err
		}
		var out []*store.Image
		for _, m := range im.Manifests {
			if !m.MediaType.IsImage() {
				continue
			}
			if m.Platform != nil && m.Platform.OS == "unknown" {
				continue // attestations
			}
			img, err := idx.Image(m.Digest)
			if err != nil {
				return nil, err
			}
			fi, err := toFlatpakImage(repo.RepositoryStr(), m.Digest.String(), m.MediaType, img, m.Platform)
			if err != nil {
				return nil, err
			}
			if fi != nil {
				fi.ListDigest = digest
				out = append(out, fi)
			}
		}
		return out, nil
	}
	if !desc.MediaType.IsImage() {
		return nil, nil
	}
	img, err := desc.Image()
	if err != nil {
		return nil, err
	}
	fi, err := toFlatpakImage(repo.RepositoryStr(), digest, desc.MediaType, img, nil)
	if err != nil || fi == nil {
		return nil, err
	}
	return []*store.Image{fi}, nil
}

// toFlatpakImage builds an index entry from an image, or returns nil when the
// image is not a flatpak.
func toFlatpakImage(repo, digest string, mediaType types.MediaType, img v1.Image, platform *v1.Platform) (*store.Image, error) {
	if !strings.HasPrefix(digest, "sha256:") {
		return nil, nil // flatpak only supports sha256 digests
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	labels := map[string]string{}
	for k, v := range cfg.Config.Labels {
		labels[k] = v
	}
	// Some tools only write flatpak metadata as manifest annotations; flatpak
	// reads labels from the index, so fold them in.
	if labels[LabelRef] == "" {
		if m, err := img.Manifest(); err == nil {
			for k, v := range m.Annotations {
				if strings.HasPrefix(k, "org.flatpak.") || strings.HasPrefix(k, "org.freedesktop.appstream.") {
					if _, ok := labels[k]; !ok {
						labels[k] = v
					}
				}
			}
		}
	}
	// Only well-formed app and runtime refs are flatpaks; anything else
	// (including values the store could not keep) is skipped and, as not a
	// flatpak, not fetched again.
	ref := labels[LabelRef]
	if !store.ValidRef(ref) {
		return nil, nil
	}

	osName, arch := cfg.OS, cfg.Architecture
	if platform != nil {
		if osName == "" {
			osName = platform.OS
		}
		if arch == "" {
			arch = platform.Architecture
		}
	}
	if osName == "" {
		osName = "linux"
	}
	if !platformValue(osName) || !platformValue(arch) {
		return nil, nil
	}

	as := parseAppstream(labels["org.freedesktop.appstream.appdata"])
	ver := firstNonEmpty(labels["version"], labels["org.opencontainers.image.version"], as.Version)

	out := &store.Image{
		Repository:    repo,
		Digest:        digest,
		MediaType:     string(mediaType),
		OS:            osName,
		Architecture:  arch,
		Ref:           ref,
		Name:          store.CleanText(as.Name),
		Summary:       store.CleanText(as.Summary),
		Version:       store.CleanText(ver),
		InstalledSize: parseSize(labels["org.flatpak.installed-size"]),
		DownloadSize:  parseSize(labels["org.flatpak.download-size"]),
		Labels:        labels,
		IndexedAt:     time.Now().UTC(),
	}
	if !cfg.Created.IsZero() && cfg.Created.Unix() > 0 {
		t := cfg.Created.UTC()
		out.Created = &t
	} else if ts, err := strconv.ParseInt(labels["org.flatpak.timestamp"], 10, 64); err == nil && ts > 0 {
		t := time.Unix(ts, 0).UTC()
		out.Created = &t
	}
	return out, nil
}

// platformValue reports whether an OS or architecture value is plausible:
// short, valid UTF-8 and without NUL (it is indexed and filtered on).
func platformValue(s string) bool {
	return len(s) <= 64 && store.CleanText(s) == s
}

func (x *Indexer) isNotFlatpak(registryID string, digest string) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.notFlatpak[registryID][digest]
}

func (x *Indexer) markNotFlatpak(registryID string, digest string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	m := x.notFlatpak[registryID]
	if m == nil {
		m = map[string]bool{}
		x.notFlatpak[registryID] = m
	}
	m[digest] = true
}

func mergeTags(a, b []string) []string {
	out := slices.Clone(a)
	for _, t := range b {
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

func parseSize(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
