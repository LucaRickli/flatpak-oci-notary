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
	out := &notaryv1.Registry{
		Id:                  r.ID,
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
		LastSyncDurationMs:  r.LastSyncDurationMs,
		ImageCount:          int32(r.ImageCount),
		RepositoryCount:     int32(r.RepositoryCount),
		CreatedAt:           ts(r.CreatedAt),
		UpdatedAt:           ts(r.UpdatedAt),
		SyncRequested:       r.SyncRequested,
	}
	if r.SyncState == store.SyncSyncing {
		out.SyncRepositoriesDone = int32(r.SyncRepositoriesDone)
		out.SyncRepositoriesTotal = int32(r.SyncRepositoriesTotal)
	}
	return out
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

func imageToProto(img *store.Image) *notaryv1.Image {
	return &notaryv1.Image{
		Id:            img.ID,
		RegistryId:    img.RegistryID,
		RegistryName:  img.RegistryName,
		Repository:    img.Repository,
		Digest:        img.Digest,
		MediaType:     img.MediaType,
		Os:            img.OS,
		Architecture:  img.Architecture,
		Tags:          img.Tags,
		Ref:           img.Ref,
		Kind:          refKindToProto(img.Kind),
		FlatpakId:     img.FlatpakID,
		Arch:          img.Arch,
		Branch:        img.Branch,
		Name:          img.Name,
		Summary:       img.Summary,
		Version:       img.Version,
		InstalledSize: img.InstalledSize,
		DownloadSize:  img.DownloadSize,
		Created:       tsPtr(img.Created),
		HasIcon:       img.HasIcon,
		IndexedAt:     ts(img.IndexedAt),
		Runtime:       img.Runtime,
		ExtensionOf:   img.ExtensionOf,
		HasExtraData:  img.HasExtraData,
	}
}

func imagesToProto(images []*store.Image) []*notaryv1.Image {
	out := make([]*notaryv1.Image, len(images))
	for i, img := range images {
		out[i] = imageToProto(img)
	}
	return out
}

func imageRepositoryToProto(r *store.ImageRepository) *notaryv1.ImageRepository {
	return &notaryv1.ImageRepository{
		RegistryId:    r.RegistryID,
		RegistryName:  r.RegistryName,
		Repository:    r.Repository,
		FlatpakIds:    r.FlatpakIDs,
		Name:          r.Name,
		Kind:          refKindToProto(r.Kind),
		ImageCount:    int32(r.ImageCount),
		Architectures: r.Architectures,
		IconImageId:   r.IconImageID,
	}
}

func registryRefsToProto(refs []store.RegistryRef) []*notaryv1.RegistryRef {
	out := make([]*notaryv1.RegistryRef, len(refs))
	for i, r := range refs {
		out[i] = &notaryv1.RegistryRef{Id: r.ID, Name: r.Name}
	}
	return out
}

func packageToProto(p *store.Package) *notaryv1.Package {
	return &notaryv1.Package{
		Kind:          refKindToProto(p.Kind),
		FlatpakId:     p.FlatpakID,
		Name:          p.Name,
		Summary:       p.Summary,
		Version:       p.Version,
		Architectures: p.Architectures,
		Branches:      p.Branches,
		Registries:    registryRefsToProto(p.Registries),
		ImageCount:    int32(p.ImageCount),
		IconImageId:   p.IconImageID,
		HasExtraData:  p.HasExtraData,
		Updated:       tsPtr(p.Updated),
	}
}

// missingRuntimesToProto pairs each missing runtime with the registries
// providing its ref ("runtime/<runtime>").
func missingRuntimesToProto(missing []flatpakindex.MissingRuntime, providers map[string][]store.RegistryRef) []*notaryv1.MissingRuntime {
	out := make([]*notaryv1.MissingRuntime, len(missing))
	for i, m := range missing {
		out[i] = &notaryv1.MissingRuntime{
			Runtime:     m.Runtime,
			NeededBy:    m.NeededBy,
			AvailableIn: registryRefsToProto(providers[store.KindRuntime+"/"+m.Runtime]),
		}
	}
	return out
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
		Id:           p.ID,
		Slug:         p.Slug,
		Title:        p.Title,
		Description:  p.Description,
		Homepage:     p.Homepage,
		RegistryId:   p.RegistryID,
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
