package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Store) repositories(ctx context.Context) *gorm.DB {
	return s.ctx(ctx).Model(&Repository{}).
		Select("repositories.*, registries.name AS registry_name").
		Joins("JOIN registries ON registries.id = repositories.registry_id")
}

func (s *Store) ListRepositories(ctx context.Context) ([]*Repository, error) {
	var out []*Repository
	if err := s.repositories(ctx).Order("repositories.slug").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) GetRepository(ctx context.Context, id string) (*Repository, error) {
	var p Repository
	if err := s.repositories(ctx).Where("repositories.id = ?", id).First(&p).Error; err != nil {
		return nil, notFound(err)
	}
	return &p, nil
}

func (s *Store) GetRepositoryBySlug(ctx context.Context, slug string) (*Repository, error) {
	var p Repository
	if err := s.repositories(ctx).Where("repositories.slug = ?", slug).First(&p).Error; err != nil {
		return nil, notFound(err)
	}
	return &p, nil
}

func (s *Store) CreateRepository(ctx context.Context, p *Repository) (*Repository, error) {
	in := *p
	in.ID = "" // generated on insert
	if in.Sources == nil {
		in.Sources = []Source{}
	}
	err := s.bumping(ctx, func(tx *gorm.DB) error {
		return tx.Omit(clause.Associations).Create(&in).Error
	})
	if err != nil {
		return nil, repositoryWriteError(err, p.Slug)
	}
	return s.GetRepository(ctx, in.ID)
}

func (s *Store) UpdateRepository(ctx context.Context, p *Repository) (*Repository, error) {
	in := *p
	if in.Sources == nil {
		in.Sources = []Source{}
	}
	err := s.bumping(ctx, func(tx *gorm.DB) error {
		res := tx.Model(&Repository{}).Where("id = ?", p.ID).
			Select("slug", "title", "description", "homepage", "registry_id", "sources", "updated_at").
			Updates(&in)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, repositoryWriteError(err, p.Slug)
	}
	return s.GetRepository(ctx, p.ID)
}

func repositoryWriteError(err error, slug string) error {
	switch {
	case isUniqueViolation(err):
		return fmt.Errorf("%w: a repository with slug %q already exists", ErrConflict, slug)
	case isForeignKeyViolation(err):
		return fmt.Errorf("%w: registry does not exist", ErrNotFound)
	default:
		return err
	}
}

func (s *Store) DeleteRepository(ctx context.Context, id string) error {
	return s.bumping(ctx, func(tx *gorm.DB) error {
		res := tx.Where("id = ?", id).Delete(&Repository{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}
