// Package indexer discovers flatpak images on upstream OCI registries and
// records them in the store.
package indexer

import (
	"context"
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
}

// Indexer syncs registries, periodically and on demand.
type Indexer struct {
	store *store.Store
	log   zerolog.Logger
	opts  Options

	mu      sync.Mutex
	running map[int64]bool
	// notFlatpak caches manifest digests known not to be flatpaks, per registry.
	notFlatpak map[int64]map[string]bool
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
	ctx, cancel := context.WithCancel(context.Background())
	return &Indexer{
		ctx:        ctx,
		cancel:     cancel,
		store:      s,
		log:        log.With().Str("component", "indexer").Logger(),
		opts:       opts,
		running:    map[int64]bool{},
		notFlatpak: map[int64]map[string]bool{},
	}
}

// Run schedules periodic syncs until ctx is cancelled, then waits for
// running syncs to stop.
func (x *Indexer) Run(ctx context.Context) {
	defer x.wg.Wait()
	defer x.cancel()
	if err := x.store.ResetInterruptedSyncs(ctx); err != nil {
		x.log.Error().Err(err).Msg("reset interrupted syncs")
	}
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

func (x *Indexer) scheduleDue(ctx context.Context) {
	regs, err := x.store.ListRegistries(ctx)
	if err != nil {
		if ctx.Err() == nil {
			x.log.Error().Err(err).Msg("list registries")
		}
		return
	}
	for _, r := range regs {
		if r.SyncIntervalMinutes <= 0 {
			continue
		}
		due := r.LastSyncAt == nil || time.Since(*r.LastSyncAt) >= time.Duration(r.SyncIntervalMinutes)*time.Minute
		if due {
			x.Trigger(ctx, r.ID)
		}
	}
}

// Trigger starts a background sync of the registry unless one is running.
// It reports whether a new sync was started.
func (x *Indexer) Trigger(ctx context.Context, registryID int64) bool {
	x.mu.Lock()
	if x.running[registryID] {
		x.mu.Unlock()
		return false
	}
	x.running[registryID] = true
	x.mu.Unlock()

	if err := x.store.SetSyncing(ctx, registryID); err != nil {
		x.log.Error().Err(err).Int64("registry_id", registryID).Msg("mark syncing")
	}

	x.wg.Add(1)
	go func() {
		defer x.wg.Done()
		defer func() {
			x.mu.Lock()
			delete(x.running, registryID)
			x.mu.Unlock()
		}()
		// Syncs outlive the triggering request but stop on shutdown.
		sctx, cancel := context.WithTimeout(x.ctx, x.opts.SyncTimeout)
		defer cancel()
		x.sync(sctx, registryID)
	}()
	return true
}

// SyncNow syncs a registry synchronously. It returns false if a sync of the
// registry is already running.
func (x *Indexer) SyncNow(ctx context.Context, registryID int64) bool {
	x.mu.Lock()
	if x.running[registryID] {
		x.mu.Unlock()
		return false
	}
	x.running[registryID] = true
	x.mu.Unlock()
	defer func() {
		x.mu.Lock()
		delete(x.running, registryID)
		x.mu.Unlock()
	}()
	if err := x.store.SetSyncing(ctx, registryID); err != nil {
		x.log.Error().Err(err).Int64("registry_id", registryID).Msg("mark syncing")
	}
	x.sync(ctx, registryID)
	return true
}

// Forget drops cached state for a deleted registry.
func (x *Indexer) Forget(registryID int64) {
	x.mu.Lock()
	delete(x.notFlatpak, registryID)
	x.mu.Unlock()
}

func (x *Indexer) sync(ctx context.Context, registryID int64) {
	start := time.Now()
	reg, err := x.store.GetRegistry(ctx, registryID)
	if err != nil {
		x.log.Error().Err(err).Int64("registry_id", registryID).Msg("load registry for sync")
		return
	}
	log := x.log.With().Int64("registry_id", reg.ID).Str("registry", reg.Name).Logger()
	log.Info().Msg("sync started")

	stats, syncErr := x.syncRegistry(ctx, reg, log)
	d := time.Since(start)
	if err := x.store.FinishSync(context.WithoutCancel(ctx), reg.ID, time.Now(), d, syncErr); err != nil {
		log.Error().Err(err).Msg("record sync result")
	}
	ev := log.Info()
	if syncErr != nil {
		ev = log.Warn().Err(syncErr)
	}
	ev.Dur("duration", d).
		Int("repositories", stats.repositories).
		Int("failed_repositories", stats.failed).
		Int("images", stats.images).
		Int("fetched_manifests", stats.fetched).
		Msg("sync finished")
}

type syncStats struct {
	repositories, failed, images, fetched int
}

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

	existing, err := x.store.ListImages(ctx, store.ImageFilter{RegistryID: reg.ID, WithLabels: true})
	if err != nil {
		return stats, err
	}
	known := map[string][]*store.Image{} // key: repository + "@" + (list) digest
	for _, row := range existing {
		img := row.Image
		known[img.Repository+"@"+img.Digest] = append(known[img.Repository+"@"+img.Digest], img)
		if img.ListDigest != "" {
			known[img.Repository+"@"+img.ListDigest] = append(known[img.Repository+"@"+img.ListDigest], img)
		}
	}

	results := make([]store.RepositoryResult, len(repos))
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(x.opts.Concurrency)
	for i, repo := range repos {
		g.Go(func() error {
			imgs, fetched, err := x.indexRepository(gctx, c, puller, reg, repo, known)
			if err != nil {
				log.Debug().Err(err).Str("repository", repo).Msg("index repository failed")
			}
			results[i] = store.RepositoryResult{Repository: repo, Images: imgs, Err: err}
			mu.Lock()
			stats.fetched += fetched
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	if err := ctx.Err(); err != nil {
		return stats, fmt.Errorf("sync aborted: %w", err)
	}

	var firstErr error
	for _, r := range results {
		if r.Err != nil {
			stats.failed++
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", r.Repository, r.Err)
			}
		}
		stats.images += len(r.Images)
	}
	if err := x.store.ReplaceRegistryImages(ctx, reg.ID, results); err != nil {
		return stats, fmt.Errorf("store images: %w", err)
	}
	if stats.failed > 0 {
		return stats, fmt.Errorf("%d of %d repositories failed, first error: %w", stats.failed, len(repos), firstErr)
	}
	return stats, nil
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
	ref := labels[LabelRef]
	if ref == "" || strings.Count(ref, "/") != 3 {
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

	as := parseAppstream(labels["org.freedesktop.appstream.appdata"])
	ver := firstNonEmpty(labels["version"], labels["org.opencontainers.image.version"], as.Version)

	out := &store.Image{
		Repository:    repo,
		Digest:        digest,
		MediaType:     string(mediaType),
		OS:            osName,
		Architecture:  arch,
		Ref:           ref,
		Name:          as.Name,
		Summary:       as.Summary,
		Version:       ver,
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

func (x *Indexer) isNotFlatpak(registryID int64, digest string) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.notFlatpak[registryID][digest]
}

func (x *Indexer) markNotFlatpak(registryID int64, digest string) {
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
