package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"

	"github.com/lucarickli/flatpak-oci-notary/internal/flatpakmeta"
)

// IconLabels are the labels carrying appstream icons, best first.
var IconLabels = []string{
	"org.freedesktop.appstream.icon-128",
	"org.freedesktop.appstream.icon-64",
}

// HasIcon reports whether the labels carry an icon.
func HasIcon(labels map[string]string) bool {
	for _, k := range IconLabels {
		if labels[k] != "" {
			return true
		}
	}
	return false
}

// imageColumns lists the image columns (derived from the model) plus the
// joined registry name, optionally without the bulky labels.
var imageColumns = func() func(withLabels bool) string {
	sch, err := schema.Parse(&Image{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		panic(err)
	}
	var all, noLabels []string
	for _, f := range sch.Fields {
		if f.DBName == "" || f.IgnoreMigration {
			continue
		}
		all = append(all, "images."+f.DBName)
		if f.DBName != "labels" {
			noLabels = append(noLabels, "images."+f.DBName)
		}
	}
	all = append(all, "registries.name AS registry_name")
	noLabels = append(noLabels, "registries.name AS registry_name")
	withL, withoutL := strings.Join(all, ", "), strings.Join(noLabels, ", ")
	return func(withLabels bool) string {
		if withLabels {
			return withL
		}
		return withoutL
	}
}()

func (s *Store) images(ctx context.Context, withLabels bool) *gorm.DB {
	return s.ctx(ctx).Model(&Image{}).Select(imageColumns(withLabels)).
		Joins("JOIN registries ON registries.id = images.registry_id")
}

// ImageFilter restricts ListImages.
type ImageFilter struct {
	// RegistryID "" means all registries.
	RegistryID string
	// Query is a case-insensitive substring of ref, name, summary or repository.
	Query string
	// Kind is KindApp, KindRuntime or empty.
	Kind string
	// FlatpakID and Architecture match exactly when set.
	FlatpakID    string
	Architecture string
	WithLabels   bool
}

func (f ImageFilter) apply(q *gorm.DB) *gorm.DB {
	if f.RegistryID != "" {
		q = q.Where("images.registry_id = ?", f.RegistryID)
	}
	if f.Kind != "" {
		q = q.Where("images.kind = ?", f.Kind)
	}
	if f.FlatpakID != "" {
		q = q.Where("images.flatpak_id = ?", f.FlatpakID)
	}
	if f.Architecture != "" {
		q = q.Where("images.architecture = ?", f.Architecture)
	}
	if f.Query != "" {
		cond, args := likeAny(f.Query, "images.ref", "images.name", "images.summary", "images.repository")
		q = q.Where(cond, args...)
	}
	return q
}

// ListImages returns one page of the images matching f, ordered by ref,
// repository and id, and the total number of matches.
func (s *Store) ListImages(ctx context.Context, f ImageFilter, p Page) ([]*Image, int, error) {
	p = p.Normalize()
	var total int64
	if err := f.apply(s.ctx(ctx).Model(&Image{})).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	out := []*Image{}
	if err := f.apply(s.images(ctx, f.WithLabels)).
		Order(s.imageOrder()).Limit(p.Size).Offset(p.Offset).
		Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, int(total), nil
}

// imageOrder orders images by ref, repository and id (byte order).
func (s *Store) imageOrder() string {
	return s.orderText("images.ref") + ", " + s.orderText("images.repository") + ", " + s.orderText("images.id")
}

// newestFirst orders the images of alias t like Newer: creation time
// (unknown last), indexing time, id. NULLS LAST is needed because Postgres
// puts NULLs first in descending order while SQLite puts them last.
func (s *Store) newestFirst(t string) string {
	return t + ".created DESC NULLS LAST, " + t + ".indexed_at DESC, " + s.orderText(t+".id") + " DESC"
}

// RegistryImages returns all images of a registry, ordered like ListImages.
func (s *Store) RegistryImages(ctx context.Context, registryID string, withLabels bool) ([]*Image, error) {
	out := []*Image{}
	err := s.images(ctx, withLabels).Where("images.registry_id = ?", registryID).
		Order(s.imageOrder()).Find(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetImage loads an image including its labels.
func (s *Store) GetImage(ctx context.Context, id string) (*Image, error) {
	var img Image
	if err := s.images(ctx, true).Where("images.id = ?", id).First(&img).Error; err != nil {
		return nil, notFound(err)
	}
	return &img, nil
}

// ---------------------------------------------------------------------------
// Image repositories

// ImageRepository summarizes the flatpak images of one OCI repository.
type ImageRepository struct {
	RegistryID   string
	RegistryName string
	Repository   string
	// FlatpakIDs are the distinct flatpak IDs of its images, sorted.
	FlatpakIDs []string
	// Name is the appstream name of one of its images; may be empty.
	Name       string
	Kind       string
	ImageCount int
	// Architectures are the distinct OCI architectures, sorted.
	Architectures []string
	// IconImageID is an image with an icon, "" if none.
	IconImageID string
}

// ImageRepositoryFilter restricts ListImageRepositories.
type ImageRepositoryFilter struct {
	// RegistryID "" means all registries.
	RegistryID string
	// Query is a case-insensitive substring of the repository, a flatpak ID
	// or a name of one of its images.
	Query string
}

func (f ImageRepositoryFilter) apply(q *gorm.DB) *gorm.DB {
	if f.RegistryID != "" {
		q = q.Where("images.registry_id = ?", f.RegistryID)
	}
	if f.Query != "" {
		cond, args := likeAny(f.Query, "images.repository", "images.flatpak_id", "images.name")
		q = q.Where(cond, args...)
	}
	return q
}

// ListImageRepositories returns one page of the distinct OCI repositories
// with flatpak images, ordered by repository, and the total count. Paging
// happens over the distinct (registry, repository) pairs in SQL; the
// summaries are then filled from all images of the selected repositories.
func (s *Store) ListImageRepositories(ctx context.Context, f ImageRepositoryFilter, p Page) ([]*ImageRepository, int, error) {
	p = p.Normalize()
	distinct := func() *gorm.DB {
		return f.apply(s.ctx(ctx).Model(&Image{}).Select("images.registry_id, images.repository")).
			Group("images.registry_id, images.repository")
	}
	var total int64
	if err := s.ctx(ctx).Raw("SELECT COUNT(*) FROM (?) AS repos", distinct()).Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	type pair struct {
		RegistryID string
		Repository string
	}
	var pairs []pair
	if err := distinct().Order(s.orderText("images.repository") + ", " + s.orderText("images.registry_id")).Limit(p.Size).Offset(p.Offset).
		Scan(&pairs).Error; err != nil {
		return nil, 0, err
	}
	out := make([]*ImageRepository, 0, len(pairs))
	if len(pairs) == 0 {
		return out, int(total), nil
	}

	byRegistry := map[string][]string{}
	for _, pr := range pairs {
		byRegistry[pr.RegistryID] = append(byRegistry[pr.RegistryID], pr.Repository)
	}
	q := s.ctx(ctx).Model(&Image{}).
		Select("images.id, images.registry_id, registries.name AS registry_name, images.repository, images.flatpak_id, images.name, images.kind, images.architecture, images.has_icon").
		Joins("JOIN registries ON registries.id = images.registry_id")
	var cond *gorm.DB
	for regID, repos := range byRegistry {
		c := s.ctx(ctx).Where("images.registry_id = ? AND images.repository IN ?", regID, repos)
		if cond == nil {
			cond = c
		} else {
			cond = cond.Or(c)
		}
	}
	var rows []Image
	if err := q.Where(cond).Order(s.orderText("images.ref") + ", " + s.orderText("images.id")).Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	index := map[pair]*ImageRepository{}
	for _, pr := range pairs {
		r := &ImageRepository{RegistryID: pr.RegistryID, Repository: pr.Repository, FlatpakIDs: []string{}, Architectures: []string{}}
		index[pr] = r
		out = append(out, r)
	}
	for i := range rows {
		img := &rows[i]
		r := index[pair{img.RegistryID, img.Repository}]
		if r == nil {
			continue
		}
		r.RegistryName = img.RegistryName
		r.ImageCount++
		if r.Kind == "" {
			r.Kind = img.Kind
		}
		if r.Name == "" {
			r.Name = img.Name
		}
		if r.IconImageID == "" && img.HasIcon {
			r.IconImageID = img.ID
		}
		if !slices.Contains(r.FlatpakIDs, img.FlatpakID) {
			r.FlatpakIDs = append(r.FlatpakIDs, img.FlatpakID)
		}
		if !slices.Contains(r.Architectures, img.Architecture) {
			r.Architectures = append(r.Architectures, img.Architecture)
		}
	}
	for _, r := range out {
		slices.Sort(r.FlatpakIDs)
		slices.Sort(r.Architectures)
	}
	return out, int(total), nil
}

// ---------------------------------------------------------------------------
// Sync results

// RepositoryResult is the outcome of indexing one OCI repository.
type RepositoryResult struct {
	Repository string
	Images     []*Image
	// Err is set when the repository could not be indexed; its existing
	// images are kept untouched.
	Err error
}

// SyncProgress is the progress of a running sync in repositories.
type SyncProgress struct {
	Done, Total int
}

// imageUpdateColumns are refreshed when an image is indexed again; the id
// is not among them, so an image keeps its id across syncs.
var imageUpdateColumns = []string{"list_digest", "media_type", "os", "architecture", "tags", "ref", "kind",
	"flatpak_id", "arch", "branch", "name", "summary", "version", "installed_size", "download_size", "created",
	"has_icon", "runtime", "extension_of", "has_extra_data", "labels", "indexed_at"}

// WriteSyncBatch stores the results of one batch of OCI repositories while
// a sync runs, in one transaction that bumps the index generation once (if
// it wrote anything), so indexed repositories become visible before the
// sync finishes. Every successfully indexed repository is replaced
// atomically (its images upserted, its stale digests removed); failed
// repositories keep their images. Images of repositories absent from the
// batch are untouched: RemoveStaleRepositories drops them once the whole
// sync completed.
//
// A repository whose images cannot be stored (invalid refs, values the
// database rejects) keeps its previous images and gets its Err set in
// results, while the other repositories are still written.
//
// When owner is set, the batch only commits if owner still holds the
// registry's sync lease (ErrLeaseLost otherwise); the lease is then renewed
// for the given duration and the progress recorded, in the same
// transaction. Batches should stay small: on SQLite the transaction holds
// the database's single write lock.
func (s *Store) WriteSyncBatch(ctx context.Context, registryID string, owner string, results []RepositoryResult,
	progress SyncProgress, lease time.Duration) error {
	return s.bumpingWhen(ctx, func(tx *gorm.DB) (bool, error) {
		written, err := writeRepositoryResults(ctx, tx, registryID, results)
		if err != nil {
			return false, err
		}
		if owner != "" {
			cols := map[string]any{
				"sync_repositories_done":  progress.Done,
				"sync_repositories_total": progress.Total,
			}
			if lease > 0 {
				cols["sync_lease_until"] = gorm.Expr(s.nowMs()+" + ?", lease.Milliseconds())
			}
			if err := verifyOwner(tx, registryID, owner, cols); err != nil {
				return false, err
			}
		}
		return written > 0, nil
	})
}

// RemoveStaleRepositories deletes the images of the registry's OCI
// repositories that are not in present, and returns how many repositories
// were dropped. A sync calls it once every repository upstream has been
// seen (discovery succeeded and all repositories were indexed or failed),
// never for an aborted sync. When owner is set the deletion only commits if
// owner still holds the sync lease (ErrLeaseLost otherwise).
func (s *Store) RemoveStaleRepositories(ctx context.Context, registryID string, owner string, present []string) (int, error) {
	var removed int
	err := s.bumpingWhen(ctx, func(tx *gorm.DB) (bool, error) {
		stale, err := staleRepositories(tx, registryID, present)
		if err != nil || len(stale) == 0 {
			return false, err
		}
		if err := deleteRepositories(tx, registryID, stale); err != nil {
			return false, err
		}
		if owner != "" {
			if err := verifyOwner(tx, registryID, owner, nil); err != nil {
				return false, err
			}
		}
		removed = len(stale)
		return true, nil
	})
	return removed, err
}

// ReplaceRegistryImages stores the result of a full registry sync in one
// transaction: images of successfully indexed repositories are replaced,
// images of failed repositories are kept and images of repositories no
// longer present are removed. It is WriteSyncBatch followed by
// RemoveStaleRepositories, atomically; syncs write incrementally instead,
// this is for seeding and tests.
//
// When owner is set, the write only happens if owner still holds the
// registry's sync lease (ErrLeaseLost otherwise).
func (s *Store) ReplaceRegistryImages(ctx context.Context, registryID string, owner string, results []RepositoryResult) error {
	return s.bumping(ctx, func(tx *gorm.DB) error {
		present := make([]string, 0, len(results))
		for _, r := range results {
			present = append(present, r.Repository)
		}
		stale, err := staleRepositories(tx, registryID, present)
		if err != nil {
			return err
		}
		if err := deleteRepositories(tx, registryID, stale); err != nil {
			return err
		}
		if _, err := writeRepositoryResults(ctx, tx, registryID, results); err != nil {
			return err
		}
		if owner != "" {
			return verifyOwner(tx, registryID, owner, nil)
		}
		return nil
	})
}

// writeRepositoryResults writes each successful result in its own
// savepoint (see WriteSyncBatch) and returns how many repositories were
// written. Repositories the store rejects get their Err set; only a
// cancelled context fails the whole write.
func writeRepositoryResults(ctx context.Context, tx *gorm.DB, registryID string, results []RepositoryResult) (int, error) {
	written := 0
	for i := range results {
		r := &results[i]
		if r.Err != nil {
			continue
		}
		rows, err := imageRows(registryID, r)
		if err == nil {
			err = tx.Transaction(func(tx *gorm.DB) error {
				return replaceRepositoryImages(tx, registryID, r.Repository, rows)
			})
		}
		if err != nil {
			if ctx.Err() != nil {
				return written, err
			}
			r.Err = fmt.Errorf("store images: %w", err)
			continue
		}
		written++
	}
	return written, nil
}

// staleRepositories returns the registry's repositories in the database
// that are not in present.
func staleRepositories(tx *gorm.DB, registryID string, present []string) ([]string, error) {
	keep := make(map[string]bool, len(present))
	for _, repo := range present {
		keep[repo] = true
	}
	var stored []string
	if err := tx.Model(&Image{}).Where("registry_id = ?", registryID).Distinct().Pluck("repository", &stored).Error; err != nil {
		return nil, err
	}
	var stale []string
	for _, repo := range stored {
		if !keep[repo] {
			stale = append(stale, repo)
		}
	}
	return stale, nil
}

// deleteRepositories removes all images of the given repositories, in
// chunks that stay below the databases' parameter limits.
func deleteRepositories(tx *gorm.DB, registryID string, repos []string) error {
	const chunk = 500
	for len(repos) > 0 {
		n := min(chunk, len(repos))
		if err := tx.Where("registry_id = ? AND repository IN ?", registryID, repos[:n]).Delete(&Image{}).Error; err != nil {
			return err
		}
		repos = repos[n:]
	}
	return nil
}

// verifyOwner fails with ErrLeaseLost unless owner holds the registry's
// sync lease, updating cols (which may be nil) on the registry row
// otherwise. It is called last in a transaction: the UPDATE locks the
// registry row until the commit, and holding it during the bulk write would
// block the lease heartbeat (and other replicas' claims) on Postgres.
func verifyOwner(tx *gorm.DB, registryID string, owner string, cols map[string]any) error {
	if cols == nil {
		cols = map[string]any{}
	}
	if len(cols) == 0 {
		cols["sync_owner"] = owner
	}
	res := tx.Model(&Registry{}).Where("id = ? AND sync_state = ? AND sync_owner = ?", registryID, SyncSyncing, owner).
		UpdateColumns(cols)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrLeaseLost
	}
	return nil
}

// imageRows validates and normalizes the images of one repository result.
// Free-text values are cleaned (NUL, invalid UTF-8); identifying values
// that are not storable fail the repository. The columns derived from the
// labels (icon, runtime, extension, extra data) are computed here, so they
// are refreshed on every sync.
func imageRows(registryID string, r *RepositoryResult) ([]Image, error) {
	if r.Repository == "" || CleanText(r.Repository) != r.Repository {
		return nil, fmt.Errorf("invalid repository name %q", r.Repository)
	}
	rows := make([]Image, 0, len(r.Images))
	for _, img := range r.Images {
		row := *img
		if !ValidRef(row.Ref) {
			return nil, fmt.Errorf("invalid flatpak ref %q in %s", CleanText(row.Ref), row.Digest)
		}
		if row.Digest == "" || CleanText(row.Digest) != row.Digest {
			return nil, fmt.Errorf("invalid digest %q", CleanText(row.Digest))
		}
		row.ID = "" // generated on insert; an upsert keeps the existing one
		row.Registry = nil
		row.RegistryID = registryID
		row.Repository = r.Repository
		row.Kind, row.FlatpakID, row.Arch, row.Branch = row.RefParts()
		row.ListDigest = CleanText(row.ListDigest)
		row.MediaType = CleanText(row.MediaType)
		row.OS = CleanText(row.OS)
		row.Architecture = CleanText(row.Architecture)
		row.Name = CleanText(row.Name)
		row.Summary = CleanText(row.Summary)
		row.Version = CleanText(row.Version)
		row.Tags = nonNil(row.Tags)
		if row.Labels == nil {
			row.Labels = map[string]string{}
		}
		row.HasIcon = HasIcon(row.Labels)
		meta := flatpakmeta.Parse(row.Labels[flatpakmeta.Label])
		row.Runtime, row.ExtensionOf, row.HasExtraData = meta.Runtime, meta.ExtensionOf, meta.HasExtraData
		row.IndexedAt = row.IndexedAt.UTC()
		row.Created = utcPtr(row.Created)
		rows = append(rows, row)
	}
	return rows, nil
}

// replaceRepositoryImages upserts the images of one repository and removes
// its images that are gone.
func replaceRepositoryImages(tx *gorm.DB, registryID string, repository string, rows []Image) error {
	digests := make([]string, 0, len(rows))
	for _, row := range rows {
		digests = append(digests, row.Digest)
	}
	if len(rows) > 0 {
		err := tx.Omit(clause.Associations).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "registry_id"}, {Name: "repository"}, {Name: "digest"}},
			DoUpdates: clause.AssignmentColumns(imageUpdateColumns),
		}).CreateInBatches(&rows, 200).Error
		if err != nil {
			return fmt.Errorf("upsert images of %s: %w", repository, err)
		}
	}
	q := tx.Where("registry_id = ? AND repository = ?", registryID, repository)
	if len(digests) > 0 {
		q = q.Where("digest NOT IN ?", digests)
	}
	return q.Delete(&Image{}).Error
}
