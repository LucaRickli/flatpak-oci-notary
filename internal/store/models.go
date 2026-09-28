package store

import (
	"strings"
	"time"
)

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

// Registry is an upstream OCI registry that gets indexed.
type Registry struct {
	ID                  int64
	Name                string
	URL                 string
	Insecure            bool
	AuthType            AuthType
	Username            string
	Password            string
	UseCatalog          bool
	Repositories        []string
	RepositoryPatterns  []string
	TagPatterns         []string
	SyncIntervalMinutes int
	SyncState           SyncState
	LastSyncAt          *time.Time
	LastSyncError       string
	LastSyncDuration    time.Duration
	CreatedAt           time.Time
	UpdatedAt           time.Time

	// Computed on read.
	ImageCount      int
	RepositoryCount int
}

// Image is an indexed flatpak image (a single-platform manifest).
type Image struct {
	ID           int64
	RegistryID   int64
	RegistryName string
	Repository   string
	Digest       string
	// ListDigest is the digest of the image index the manifest was found in, if any.
	ListDigest    string
	MediaType     string
	OS            string
	Architecture  string
	Tags          []string
	Ref           string
	Name          string
	Summary       string
	Version       string
	InstalledSize int64
	DownloadSize  int64
	Created       *time.Time
	Labels        map[string]string
	IndexedAt     time.Time
}

// RefParts splits the flatpak ref "kind/id/arch/branch".
func (i *Image) RefParts() (kind, id, arch, branch string) {
	parts := strings.SplitN(i.Ref, "/", 4)
	for len(parts) < 4 {
		parts = append(parts, "")
	}
	return parts[0], parts[1], parts[2], parts[3]
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
	ID           int64
	Slug         string
	Title        string
	Description  string
	Homepage     string
	RegistryID   int64
	RegistryName string
	Sources      []Source
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
