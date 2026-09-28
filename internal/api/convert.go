package api

import (
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/lucarickli/flatpak-oci-notary/internal/flatpakindex"
	notaryv1 "github.com/lucarickli/flatpak-oci-notary/internal/gen/notary/v1"
	"github.com/lucarickli/flatpak-oci-notary/internal/store"
)

func ts(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func tsPtr(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func authTypeToProto(t store.AuthType) notaryv1.AuthType {
	if t == store.AuthBasic {
		return notaryv1.AuthType_AUTH_TYPE_BASIC
	}
	return notaryv1.AuthType_AUTH_TYPE_ANONYMOUS
}

func syncStateToProto(s store.SyncState) notaryv1.SyncState {
	switch s {
	case store.SyncSyncing:
		return notaryv1.SyncState_SYNC_STATE_SYNCING
	case store.SyncOK:
		return notaryv1.SyncState_SYNC_STATE_OK
	case store.SyncError:
		return notaryv1.SyncState_SYNC_STATE_ERROR
	default:
		return notaryv1.SyncState_SYNC_STATE_NEVER
	}
}

func registryToProto(r *store.Registry) *notaryv1.Registry {
	return &notaryv1.Registry{
		Id:                  int32(r.ID),
		Name:                r.Name,
		Url:                 r.URL,
		Insecure:            r.Insecure,
		AuthType:            authTypeToProto(r.AuthType),
		Username:            r.Username,
		HasPassword:         r.Password != "",
		UseCatalog:          r.UseCatalog,
		Repositories:        r.Repositories,
		RepositoryPatterns:  r.RepositoryPatterns,
		TagPatterns:         r.TagPatterns,
		SyncIntervalMinutes: int32(r.SyncIntervalMinutes),
		SyncState:           syncStateToProto(r.SyncState),
		LastSyncAt:          tsPtr(r.LastSyncAt),
		LastSyncError:       r.LastSyncError,
		LastSyncDurationMs:  r.LastSyncDuration.Milliseconds(),
		ImageCount:          int32(r.ImageCount),
		RepositoryCount:     int32(r.RepositoryCount),
		CreatedAt:           ts(r.CreatedAt),
		UpdatedAt:           ts(r.UpdatedAt),
	}
}

func refKindToProto(kind string) notaryv1.RefKind {
	switch kind {
	case "app":
		return notaryv1.RefKind_REF_KIND_APP
	case "runtime":
		return notaryv1.RefKind_REF_KIND_RUNTIME
	default:
		return notaryv1.RefKind_REF_KIND_UNSPECIFIED
	}
}

func imageToProto(img *store.Image, hasIcon bool) *notaryv1.Image {
	kind, id, arch, branch := img.RefParts()
	return &notaryv1.Image{
		Id:            int32(img.ID),
		RegistryId:    int32(img.RegistryID),
		RegistryName:  img.RegistryName,
		Repository:    img.Repository,
		Digest:        img.Digest,
		MediaType:     img.MediaType,
		Os:            img.OS,
		Architecture:  img.Architecture,
		Tags:          img.Tags,
		Ref:           img.Ref,
		Kind:          refKindToProto(kind),
		FlatpakId:     id,
		Arch:          arch,
		Branch:        branch,
		Name:          img.Name,
		Summary:       img.Summary,
		Version:       img.Version,
		InstalledSize: img.InstalledSize,
		DownloadSize:  img.DownloadSize,
		Created:       tsPtr(img.Created),
		HasIcon:       hasIcon,
		IndexedAt:     ts(img.IndexedAt),
	}
}

// strippedLabels returns labels without bulky icon data URIs.
func strippedLabels(labels map[string]string) map[string]string {
	out := make(map[string]string, len(labels))
	for k, v := range labels {
		if strings.HasPrefix(v, "data:") && strings.HasPrefix(k, "org.freedesktop.appstream.icon-") {
			v = "(icon data, " + humanBytes(len(v)) + ")"
		}
		out[k] = v
	}
	return out
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func sourceToProto(s store.Source) *notaryv1.Source {
	return &notaryv1.Source{
		RepositoryPattern: s.RepositoryPattern,
		RefPattern:        s.RefPattern,
		TagPattern:        s.TagPattern,
		Exclude:           s.Exclude,
	}
}

func repositoryToProto(p *store.Repository, baseURL string, imageCount int) *notaryv1.Repository {
	sources := make([]*notaryv1.Source, len(p.Sources))
	for i, s := range p.Sources {
		sources[i] = sourceToProto(s)
	}
	urls := flatpakindex.RepositoryURLs(baseURL, p.Slug)
	return &notaryv1.Repository{
		Id:           int32(p.ID),
		Slug:         p.Slug,
		Title:        p.Title,
		Description:  p.Description,
		Homepage:     p.Homepage,
		RegistryId:   int32(p.RegistryID),
		RegistryName: p.RegistryName,
		Sources:      sources,
		ImageCount:   int32(imageCount),
		Urls: &notaryv1.RepositoryUrls{
			Remote:      urls.Remote,
			Flatpakrepo: urls.Flatpakrepo,
			Index:       urls.Index,
		},
		CreatedAt: ts(p.CreatedAt),
		UpdatedAt: ts(p.UpdatedAt),
	}
}
