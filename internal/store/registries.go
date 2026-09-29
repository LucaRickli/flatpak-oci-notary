package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// registrySelect adds the per-registry counts and whether the sync lease
// expired (by the database clock).
const registrySelect = `registries.*,
	(SELECT COUNT(*) FROM images WHERE images.registry_id = registries.id) AS image_count,
	(SELECT COUNT(DISTINCT images.repository) FROM images WHERE images.registry_id = registries.id) AS repository_count,
	(registries.sync_state = 'syncing' AND registries.sync_lease_until < %s) AS lease_expired`

func (s *Store) registries(ctx context.Context) *gorm.DB {
	return s.ctx(ctx).Model(&Registry{}).Select(fmt.Sprintf(registrySelect, s.nowMs()))
}

func (s *Store) ListRegistries(ctx context.Context) ([]*Registry, error) {
	var out []*Registry
	if err := s.registries(ctx).Order("registries.name").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) GetRegistry(ctx context.Context, id string) (*Registry, error) {
	var r Registry
	if err := s.registries(ctx).Where("registries.id = ?", id).First(&r).Error; err != nil {
		return nil, notFound(err)
	}
	return &r, nil
}

// CreateRegistry stores a new registry. Sync state is reset; SyncRequested
// is kept as given, so a caller can ask for the first sync at once.
func (s *Store) CreateRegistry(ctx context.Context, r *Registry) (*Registry, error) {
	in := *r
	in.ID = "" // generated on insert
	in.Repositories = nonNil(in.Repositories)
	in.RepositoryPatterns = nonNil(in.RepositoryPatterns)
	in.TagPatterns = nonNil(in.TagPatterns)
	in.SyncState = SyncNever
	in.SyncOwner, in.SyncLeaseUntil = "", 0
	in.SyncRepositoriesDone, in.SyncRepositoriesTotal = 0, 0
	in.LastSyncAt, in.LastSyncError, in.LastSyncDurationMs = nil, "", 0
	err := s.ctx(ctx).Omit(clause.Associations).Create(&in).Error
	if isUniqueViolation(err) {
		return nil, fmt.Errorf("%w: a registry named %q already exists", ErrConflict, r.Name)
	}
	if err != nil {
		return nil, err
	}
	return s.GetRegistry(ctx, in.ID)
}

// UpdateRegistry updates the configuration of a registry. The password is
// only changed when setPassword is true. SyncRequested is written too, so a
// configuration change can ask for a resync in the same transaction.
func (s *Store) UpdateRegistry(ctx context.Context, r *Registry, setPassword bool) (*Registry, error) {
	cols := []string{"name", "url", "insecure", "auth_type", "username", "use_catalog", "repositories",
		"repository_patterns", "tag_patterns", "sync_interval_minutes", "sync_requested", "updated_at"}
	if setPassword {
		cols = append(cols, "password")
	}
	in := *r
	in.Repositories = nonNil(in.Repositories)
	in.RepositoryPatterns = nonNil(in.RepositoryPatterns)
	in.TagPatterns = nonNil(in.TagPatterns)
	err := s.bumping(ctx, func(tx *gorm.DB) error {
		res := tx.Model(&Registry{}).Where("id = ?", r.ID).Select(cols).Updates(&in)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
	if isUniqueViolation(err) {
		return nil, fmt.Errorf("%w: a registry named %q already exists", ErrConflict, r.Name)
	}
	if err != nil {
		return nil, err
	}
	return s.GetRegistry(ctx, r.ID)
}

// DeleteRegistry deletes a registry and its images. A registry used by a
// repository cannot be deleted (ErrConflict).
func (s *Store) DeleteRegistry(ctx context.Context, id string) error {
	err := s.bumping(ctx, func(tx *gorm.DB) error {
		res := tx.Where("id = ?", id).Delete(&Registry{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
	if isForeignKeyViolation(err) {
		return fmt.Errorf("%w: registry is used by at least one repository", ErrConflict)
	}
	return err
}

// ---------------------------------------------------------------------------
// Overview

type Overview struct {
	Registries, Repositories, Images, Apps, Runtimes int
}

func (s *Store) Overview(ctx context.Context) (Overview, error) {
	var o Overview
	err := s.ctx(ctx).Raw(`SELECT
		(SELECT COUNT(*) FROM registries) AS registries,
		(SELECT COUNT(*) FROM repositories) AS repositories,
		(SELECT COUNT(*) FROM images) AS images,
		(SELECT COUNT(DISTINCT flatpak_id) FROM images WHERE kind = ?) AS apps,
		(SELECT COUNT(DISTINCT flatpak_id) FROM images WHERE kind = ?) AS runtimes`, KindApp, KindRuntime).Scan(&o).Error
	return o, err
}
