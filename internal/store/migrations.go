package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// Migrations are versioned and portable across dialects. Each migration
// freezes the schema it creates in its own snapshot types (never the live
// models), so later model changes must come with a new migration. While the
// project is unreleased the initial migration is rewritten in place under a
// new id instead; databases created by a superseded id are rejected with a
// message asking to delete them (see migrate).
var migrations = []*gormigrate.Migration{
	{
		ID: "20260930_initial",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Migrator().CreateTable(&registryV1{}, &imageV1{}, &repositoryV1{}, &indexStateV1{}); err != nil {
				return err
			}
			return tx.Create(&indexStateV1{ID: 1, Generation: 1}).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Migrator().DropTable(&indexStateV1{}, &repositoryV1{}, &imageV1{}, &registryV1{})
		},
	},
}

// pgMigrationLock is the advisory lock key serializing migrations of
// replicas that start concurrently against one Postgres database.
const pgMigrationLock = 7301589

func (s *Store) migrate(ctx context.Context) error {
	db := s.ctx(ctx)
	if db.Migrator().HasTable("schema_version") && !db.Migrator().HasTable("migrations") {
		return fmt.Errorf("the database was created by an older, incompatible version: delete it (e.g. data/notary.db) and start again")
	}
	opts := &gormigrate.Options{
		TableName:                 "migrations",
		IDColumnName:              "id",
		IDColumnSize:              255,
		UseTransaction:            true,
		ValidateUnknownMigrations: true,
	}
	run := func(db *gorm.DB) error {
		err := gormigrate.New(db, opts, migrations).Migrate()
		if errors.Is(err, gormigrate.ErrUnknownPastMigration) {
			// A migration id this version does not know: the database was
			// created by a superseded development version.
			return fmt.Errorf("the database was created by an older, incompatible version: delete it (e.g. data/notary.db) and start again")
		}
		return err
	}
	if s.Driver != DriverPostgres {
		return run(db)
	}
	// Migrations must run on the connection holding the lock.
	return db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("SELECT pg_advisory_lock(?)", pgMigrationLock).Error; err != nil {
			return err
		}
		defer conn.Exec("SELECT pg_advisory_unlock(?)", pgMigrationLock)
		return run(conn)
	})
}

// ---------------------------------------------------------------------------
// Schema snapshots

type registryV1 struct {
	ID                    string   `gorm:"primaryKey;size:36"`
	Name                  string   `gorm:"size:100;not null;uniqueIndex"`
	URL                   string   `gorm:"not null"`
	Insecure              bool     `gorm:"not null;default:false"`
	AuthType              string   `gorm:"size:16;not null;default:anonymous"`
	Username              string   `gorm:"not null;default:''"`
	Password              string   `gorm:"not null;default:''"`
	UseCatalog            bool     `gorm:"not null;default:false"`
	Repositories          []string `gorm:"serializer:json;type:text;not null"`
	RepositoryPatterns    []string `gorm:"serializer:json;type:text;not null"`
	TagPatterns           []string `gorm:"serializer:json;type:text;not null"`
	SyncIntervalMinutes   int      `gorm:"not null"`
	SyncState             string   `gorm:"size:16;not null;default:never"`
	SyncOwner             string   `gorm:"size:64;not null;default:''"`
	SyncLeaseUntil        int64    `gorm:"not null;default:0"`
	SyncRequested         bool     `gorm:"not null;default:false"`
	SyncRepositoriesDone  int      `gorm:"not null;default:0"`
	SyncRepositoriesTotal int      `gorm:"not null;default:0"`
	LastSyncAt            *time.Time
	LastSyncError         string    `gorm:"not null;default:''"`
	LastSyncDurationMs    int64     `gorm:"not null;default:0"`
	CreatedAt             time.Time `gorm:"not null"`
	UpdatedAt             time.Time `gorm:"not null"`
}

func (registryV1) TableName() string { return "registries" }

type imageV1 struct {
	ID            string      `gorm:"primaryKey;size:36"`
	RegistryID    string      `gorm:"size:36;not null;uniqueIndex:idx_images_registry_repository_digest,priority:1;index:idx_images_registry_repository,priority:1"`
	Registry      *registryV1 `gorm:"foreignKey:RegistryID;constraint:OnDelete:CASCADE"`
	Repository    string      `gorm:"not null;uniqueIndex:idx_images_registry_repository_digest,priority:2;index:idx_images_registry_repository,priority:2"`
	Digest        string      `gorm:"not null;uniqueIndex:idx_images_registry_repository_digest,priority:3"`
	ListDigest    string      `gorm:"not null"`
	MediaType     string      `gorm:"not null"`
	OS            string      `gorm:"not null"`
	Architecture  string      `gorm:"not null;index"`
	Tags          []string    `gorm:"serializer:json;type:text;not null"`
	Ref           string      `gorm:"not null;index"`
	Kind          string      `gorm:"size:16;not null;index:idx_images_kind_flatpak_id,priority:1"`
	FlatpakID     string      `gorm:"not null;index;index:idx_images_kind_flatpak_id,priority:2"`
	Arch          string      `gorm:"not null"`
	Branch        string      `gorm:"not null"`
	Name          string      `gorm:"not null"`
	Summary       string      `gorm:"not null"`
	Version       string      `gorm:"not null"`
	InstalledSize int64       `gorm:"not null"`
	DownloadSize  int64       `gorm:"not null"`
	Created       *time.Time
	HasIcon       bool              `gorm:"not null"`
	Runtime       string            `gorm:"not null;default:''"`
	ExtensionOf   string            `gorm:"not null;default:''"`
	HasExtraData  bool              `gorm:"not null;default:false"`
	Labels        map[string]string `gorm:"serializer:json;type:text;not null"`
	IndexedAt     time.Time         `gorm:"not null"`
}

func (imageV1) TableName() string { return "images" }

type repositoryV1 struct {
	ID          string      `gorm:"primaryKey;size:36"`
	Slug        string      `gorm:"size:63;not null;uniqueIndex"`
	Title       string      `gorm:"not null;default:''"`
	Description string      `gorm:"not null;default:''"`
	Homepage    string      `gorm:"not null;default:''"`
	RegistryID  string      `gorm:"size:36;not null;index"`
	Registry    *registryV1 `gorm:"foreignKey:RegistryID;constraint:OnDelete:RESTRICT"`
	Sources     []Source    `gorm:"serializer:json;type:text;not null"`
	CreatedAt   time.Time   `gorm:"not null"`
	UpdatedAt   time.Time   `gorm:"not null"`
}

func (repositoryV1) TableName() string { return "repositories" }

type indexStateV1 struct {
	ID         int64 `gorm:"primaryKey"`
	Generation int64 `gorm:"not null"`
}

func (indexStateV1) TableName() string { return "index_state" }
