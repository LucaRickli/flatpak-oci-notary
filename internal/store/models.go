package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// NewID returns a new UUIDv7 in canonical string form. Ids are generated in
// Go on insert, so they are portable across dialects and sort by creation
// time.
func NewID() string {
	id, err := uuid.NewV7()
	if err != nil {
		panic(fmt.Errorf("generate uuid: %w", err))
	}
	return id.String()
}

// ParseID validates an id from an untrusted source (a request) and returns
// its canonical form. Ids are stored as text, so a malformed id passed to a
// lookup is merely not found; callers validate to report it as such.
func ParseID(s string) (string, error) {
	u, err := uuid.Parse(strings.TrimSpace(s))
	if err != nil {
		return "", fmt.Errorf("invalid id %q", s)
	}
	return u.String(), nil
}

// newID fills an empty id before an insert (GORM BeforeCreate hook).
func newID(id *string) error {
	if *id == "" {
		*id = NewID()
	}
	return nil
}

type AuthType string

const (
	AuthAnonymous AuthType = "anonymous"
	AuthBasic     AuthType = "basic"
)

type SyncState string

const (
	SyncNever   SyncState = "never"
	SyncSyncing SyncState = "syncing"
	SyncOK      SyncState = "ok"
	SyncError   SyncState = "error"
)

// MaxSyncOwnerLength is the size of the sync owner column: instance ids
// must not be longer.
const MaxSyncOwnerLength = 64

// Ref kinds, the first component of a flatpak ref.
const (
	KindApp     = "app"
	KindRuntime = "runtime"
)

// Registry is an upstream OCI registry that gets indexed.
type Registry struct {
	ID                  string    `gorm:"primaryKey;size:36"`
	Name                string    `gorm:"size:100;not null;uniqueIndex"`
	URL                 string    `gorm:"not null"`
	Insecure            bool      `gorm:"not null;default:false"`
	AuthType            AuthType  `gorm:"size:16;not null;default:anonymous"`
	Username            string    `gorm:"not null;default:''"`
	Password            string    `gorm:"not null;default:''"`
	UseCatalog          bool      `gorm:"not null;default:false"`
	Repositories        []string  `gorm:"serializer:json;type:text;not null"`
	RepositoryPatterns  []string  `gorm:"serializer:json;type:text;not null"`
	TagPatterns         []string  `gorm:"serializer:json;type:text;not null"`
	SyncIntervalMinutes int       `gorm:"not null"`
	SyncState           SyncState `gorm:"size:16;not null;default:never"`
	// SyncOwner is the instance holding the sync lease while SyncState is
	// "syncing" (at most MaxSyncOwnerLength bytes).
	SyncOwner string `gorm:"size:64;not null;default:''"`
	// SyncLeaseUntil is the lease expiry in Unix milliseconds (0 = none).
	// Stored as an integer so comparisons are portable across dialects.
	SyncLeaseUntil int64 `gorm:"not null;default:0"`
	// SyncRequested is set when a sync was asked for (SyncRegistry, or the
	// registry was created or changed) and cleared when a syncer claims the
	// sync. Requested registries are due regardless of their interval; if the
	// claimed sync is interrupted, the registry stays due as interrupted
	// (see SyncInterrupted), so the request is not lost.
	SyncRequested bool `gorm:"not null;default:false"`
	// SyncRepositoriesDone and SyncRepositoriesTotal are the progress of the
	// running sync: total is set once the repositories are discovered (0
	// before), done grows as batches of repositories are written. Both are
	// reset when a sync is claimed and keep their final values afterwards.
	SyncRepositoriesDone  int `gorm:"not null;default:0"`
	SyncRepositoriesTotal int `gorm:"not null;default:0"`
	LastSyncAt            *time.Time
	LastSyncError         string    `gorm:"not null;default:''"`
	LastSyncDurationMs    int64     `gorm:"not null;default:0"`
	CreatedAt             time.Time `gorm:"not null"`
	UpdatedAt             time.Time `gorm:"not null"`

	// Computed on read.
	ImageCount      int `gorm:"->;-:migration"`
	RepositoryCount int `gorm:"->;-:migration"`
	// LeaseExpired is set when the sync lease ran out by the database clock.
	LeaseExpired bool `gorm:"->;-:migration"`
}

// BeforeCreate generates the id.
func (r *Registry) BeforeCreate(*gorm.DB) error { return newID(&r.ID) }

// LastSyncDuration is the duration of the last sync.
func (r *Registry) LastSyncDuration() time.Duration {
	return time.Duration(r.LastSyncDurationMs) * time.Millisecond
}

// AfterFind normalizes loaded rows: slices are never nil, times are UTC and
// a sync whose lease expired is reported as interrupted (LeaseExpiredError).
func (r *Registry) AfterFind(*gorm.DB) error {
	r.Repositories = nonNil(r.Repositories)
	r.RepositoryPatterns = nonNil(r.RepositoryPatterns)
	r.TagPatterns = nonNil(r.TagPatterns)
	r.CreatedAt = r.CreatedAt.UTC()
	r.UpdatedAt = r.UpdatedAt.UTC()
	r.LastSyncAt = utcPtr(r.LastSyncAt)
	if r.SyncState == SyncSyncing && r.LeaseExpired {
		r.SyncState = SyncError
		r.LastSyncError = LeaseExpiredError
	}
	return nil
}

// SyncInterrupted reports whether the last sync stopped before it finished
// (see LeaseExpiredError, ShutdownInterruptedError, RestartInterruptedError).
func (r *Registry) SyncInterrupted() bool {
	if r.SyncState != SyncError {
		return false
	}
	switch r.LastSyncError {
	case LeaseExpiredError, ShutdownInterruptedError, RestartInterruptedError:
		return true
	}
	return false
}

// Image is an indexed flatpak image (a single-platform manifest).
type Image struct {
	ID         string    `gorm:"primaryKey;size:36"`
	RegistryID string    `gorm:"size:36;not null;uniqueIndex:idx_images_registry_repository_digest,priority:1;index:idx_images_registry_repository,priority:1"`
	Registry   *Registry `gorm:"constraint:OnDelete:CASCADE"`
	// RegistryName is joined in on read.
	RegistryName string `gorm:"->;-:migration"`
	// Repository is the OCI repository path, e.g. "myorg/org.example.App".
	Repository string `gorm:"not null;uniqueIndex:idx_images_registry_repository_digest,priority:2;index:idx_images_registry_repository,priority:2"`
	Digest     string `gorm:"not null;uniqueIndex:idx_images_registry_repository_digest,priority:3"`
	// ListDigest is the digest of the image index the manifest was found in, if any.
	ListDigest   string   `gorm:"not null"`
	MediaType    string   `gorm:"not null"`
	OS           string   `gorm:"not null"`
	Architecture string   `gorm:"not null;index"`
	Tags         []string `gorm:"serializer:json;type:text;not null"`
	// Ref is the full flatpak ref, e.g. "app/org.example.App/x86_64/stable".
	// Kind, FlatpakID, Arch and Branch are its components, denormalized for
	// filtering and counting in SQL.
	Ref           string `gorm:"not null;index"`
	Kind          string `gorm:"size:16;not null;index:idx_images_kind_flatpak_id,priority:1"`
	FlatpakID     string `gorm:"not null;index;index:idx_images_kind_flatpak_id,priority:2"`
	Arch          string `gorm:"not null"`
	Branch        string `gorm:"not null"`
	Name          string `gorm:"not null"`
	Summary       string `gorm:"not null"`
	Version       string `gorm:"not null"`
	InstalledSize int64  `gorm:"not null"`
	DownloadSize  int64  `gorm:"not null"`
	Created       *time.Time
	// HasIcon reports whether Labels carry an appstream icon.
	HasIcon bool `gorm:"not null"`
	// Runtime, ExtensionOf and HasExtraData are derived from the flatpak
	// metadata keyfile in Labels (see flatpakmeta.Parse) when the image is
	// stored: the runtime ref ("<id>/<arch>/<branch>") the flatpak needs,
	// the ref an extension extends, and whether installing downloads extra
	// data from external URLs.
	Runtime      string `gorm:"not null;default:''"`
	ExtensionOf  string `gorm:"not null;default:''"`
	HasExtraData bool   `gorm:"not null;default:false"`
	// Labels is nil when loaded without labels.
	Labels    map[string]string `gorm:"serializer:json;type:text;not null"`
	IndexedAt time.Time         `gorm:"not null"`
}

// BeforeCreate generates the id.
func (i *Image) BeforeCreate(*gorm.DB) error { return newID(&i.ID) }

// RuntimeRef is the full ref of the runtime the image needs
// ("runtime/<id>/<arch>/<branch>"), or "" when it needs none.
func (i *Image) RuntimeRef() string {
	if i.Runtime == "" {
		return ""
	}
	return KindRuntime + "/" + i.Runtime
}

// Newer reports whether a is a newer build than b: by creation time (unknown
// counts as oldest), then by indexing time, then by id (UUIDv7, so later
// inserts win). Every place that picks "the newest image" uses this order,
// and the SQL equivalent is newestFirst. Times are compared as time values,
// never as Unix nanoseconds, which overflow for dates after 2262 (upstream
// creation dates are not validated).
func Newer(a, b *Image) bool {
	switch {
	case a.Created == nil && b.Created != nil:
		return false
	case a.Created != nil && b.Created == nil:
		return true
	case a.Created != nil && !a.Created.Equal(*b.Created):
		return a.Created.After(*b.Created)
	}
	if !a.IndexedAt.Equal(b.IndexedAt) {
		return a.IndexedAt.After(b.IndexedAt)
	}
	return a.ID > b.ID
}

// MaxRefLength bounds a flatpak ref in bytes (flatpak limits IDs to 255
// characters), well below the size limits of database indexes.
const MaxRefLength = 1024

// ValidRef reports whether ref is a flatpak ref the store accepts:
// "app/<id>/<arch>/<branch>" or "runtime/<id>/<arch>/<branch>" with
// non-empty components, valid UTF-8 without NUL, at most MaxRefLength bytes.
func ValidRef(ref string) bool {
	if len(ref) > MaxRefLength || CleanText(ref) != ref {
		return false
	}
	parts := strings.Split(ref, "/")
	if len(parts) != 4 || (parts[0] != KindApp && parts[0] != KindRuntime) {
		return false
	}
	for _, p := range parts[1:] {
		if p == "" {
			return false
		}
	}
	return true
}

// RefParts splits the flatpak ref "kind/id/arch/branch".
func (i *Image) RefParts() (kind, id, arch, branch string) {
	parts := strings.SplitN(i.Ref, "/", 4)
	for len(parts) < 4 {
		parts = append(parts, "")
	}
	return parts[0], parts[1], parts[2], parts[3]
}

// AfterFind normalizes loaded rows.
func (i *Image) AfterFind(*gorm.DB) error {
	i.Tags = nonNil(i.Tags)
	i.Created = utcPtr(i.Created)
	i.IndexedAt = i.IndexedAt.UTC()
	return nil
}

// Source is a rule selecting images for a repository.
type Source struct {
	RepositoryPattern string `json:"repositoryPattern"`
	RefPattern        string `json:"refPattern"`
	TagPattern        string `json:"tagPattern"`
	Exclude           bool   `json:"exclude"`
}

// Repository is a published flatpak remote backed by one registry.
type Repository struct {
	ID          string    `gorm:"primaryKey;size:36"`
	Slug        string    `gorm:"size:63;not null;uniqueIndex"`
	Title       string    `gorm:"not null;default:''"`
	Description string    `gorm:"not null;default:''"`
	Homepage    string    `gorm:"not null;default:''"`
	RegistryID  string    `gorm:"size:36;not null;index"`
	Registry    *Registry `gorm:"constraint:OnDelete:RESTRICT"`
	// RegistryName is joined in on read.
	RegistryName string    `gorm:"->;-:migration"`
	Sources      []Source  `gorm:"serializer:json;type:text;not null"`
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

// BeforeCreate generates the id.
func (p *Repository) BeforeCreate(*gorm.DB) error { return newID(&p.ID) }

// AfterFind normalizes loaded rows.
func (p *Repository) AfterFind(*gorm.DB) error {
	if p.Sources == nil {
		p.Sources = []Source{}
	}
	p.CreatedAt = p.CreatedAt.UTC()
	p.UpdatedAt = p.UpdatedAt.UTC()
	return nil
}

// indexState is a single row (id 1) whose generation changes whenever data
// affecting served indexes changes. All replicas share it.
type indexState struct {
	ID         int64 `gorm:"primaryKey"`
	Generation int64 `gorm:"not null"`
}

func (indexState) TableName() string { return "index_state" }

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
