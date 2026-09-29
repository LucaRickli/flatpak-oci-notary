package server_test

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/rs/zerolog"

	notaryv1 "github.com/lucarickli/flatpak-oci-notary/internal/gen/notary/v1"
	"github.com/lucarickli/flatpak-oci-notary/internal/gen/notary/v1/notaryv1connect"
	"github.com/lucarickli/flatpak-oci-notary/internal/indexer"
	"github.com/lucarickli/flatpak-oci-notary/internal/server"
	"github.com/lucarickli/flatpak-oci-notary/internal/store"
	"github.com/lucarickli/flatpak-oci-notary/internal/store/storetest"
)

const flatpakQuery = "label%3Aorg.flatpak.ref%3Aexists=1&architecture=amd64&os=linux&tag=latest"

type imageSpec struct {
	ref, arch string
	created   time.Time
	// annotationsOnly puts the flatpak metadata into manifest annotations
	// instead of config labels.
	annotationsOnly bool
	// runtime is written to the metadata keyfile (runtime=), extraData adds
	// an [Extra Data] group.
	runtime   string
	extraData bool
}

func makeImage(t *testing.T, s imageSpec) v1.Image {
	t.Helper()
	img, err := random.Image(128, 1)
	must(t, err)
	img = mutate.MediaType(img, types.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, types.OCIConfigJSON)
	cfg, err := img.ConfigFile()
	must(t, err)
	cfg = cfg.DeepCopy()
	cfg.OS, cfg.Architecture, cfg.Created = "linux", s.arch, v1.Time{Time: s.created}
	labels := map[string]string{}
	if s.ref != "" {
		id := strings.Split(s.ref, "/")[1]
		group := "[Application]"
		if strings.HasPrefix(s.ref, "runtime/") {
			group = "[Runtime]"
		}
		metadata := group + "\nname=" + id + "\n"
		if s.runtime != "" {
			metadata += "runtime=" + s.runtime + "\n"
		}
		if s.extraData {
			metadata += "\n[Extra Data]\nname=extra.bin\nchecksum=00\nsize=1\nuri=https://example.com/extra.bin\n"
		}
		labels = map[string]string{
			"org.flatpak.ref":            s.ref,
			"org.flatpak.metadata":       metadata,
			"org.flatpak.installed-size": "4096",
			"org.flatpak.download-size":  "1024",
			"org.freedesktop.appstream.appdata": `<?xml version="1.0"?><components><component type="desktop-application">` +
				`<id>` + id + `</id><name>Hello</name><name xml:lang="de">Hallo</name><summary>Says hello</summary>` +
				`<releases><release version="1.2.3"/></releases></component></components>`,
			"org.freedesktop.appstream.icon-64": "data:image/png;base64,iVBORw0KGgo=",
		}
	}
	if s.annotationsOnly {
		cfg.Config.Labels = nil
		img, err = mutate.ConfigFile(img, cfg)
		must(t, err)
		return mutate.Annotations(img, labels).(v1.Image)
	}
	cfg.Config.Labels = labels
	img, err = mutate.ConfigFile(img, cfg)
	must(t, err)
	return img
}

func push(t *testing.T, host, ref string, img v1.Image) v1.Hash {
	t.Helper()
	r, err := name.ParseReference(host+"/"+ref, name.Insecure)
	must(t, err)
	must(t, remote.Write(r, img))
	d, err := img.Digest()
	must(t, err)
	return d
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// TestEndToEnd indexes an in-memory OCI registry and checks the served
// flatpak index and the admin API, on every supported database.
func TestEndToEnd(t *testing.T) {
	storetest.Run(t, testEndToEnd)
}

func testEndToEnd(t *testing.T, tg storetest.Target) {
	ctx := context.Background()
	reg := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer reg.Close()
	host := strings.TrimPrefix(reg.URL, "http://")

	old := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	const platform = "org.example.Platform/x86_64/stable"

	helloLatest := push(t, host, "apps/hello:latest", makeImage(t, imageSpec{ref: "app/org.example.Hello/x86_64/stable", arch: "amd64", created: newer, runtime: platform}))
	push(t, host, "apps/hello:old", makeImage(t, imageSpec{ref: "app/org.example.Hello/x86_64/stable", arch: "amd64", created: old, runtime: platform}))
	push(t, host, "apps/annotated:latest", makeImage(t, imageSpec{ref: "app/org.example.Annotated/x86_64/stable", arch: "amd64", created: newer, annotationsOnly: true, runtime: platform}))
	push(t, host, "containers/busybox:latest", makeImage(t, imageSpec{arch: "amd64", created: newer}))
	push(t, host, "apps/beta:beta", makeImage(t, imageSpec{ref: "app/org.example.Beta/x86_64/beta", arch: "amd64", created: newer, runtime: platform}))
	push(t, host, "apps/hidden:latest", makeImage(t, imageSpec{ref: "app/org.example.Hidden/x86_64/stable", arch: "amd64", created: newer, runtime: platform}))
	push(t, host, "runtimes/platform:latest", makeImage(t, imageSpec{ref: "runtime/" + platform, arch: "amd64", created: newer}))
	push(t, host, "runtimes/codecs:latest", makeImage(t, imageSpec{ref: "runtime/org.example.Platform.Codecs/x86_64/stable", arch: "amd64", created: newer, runtime: platform, extraData: true}))

	amd := makeImage(t, imageSpec{ref: "app/org.example.Multi/x86_64/stable", arch: "amd64", created: newer, runtime: platform})
	arm := makeImage(t, imageSpec{ref: "app/org.example.Multi/aarch64/stable", arch: "arm64", created: newer, runtime: "org.example.Platform/aarch64/stable"})
	idx := mutate.AppendManifests(mutate.IndexMediaType(empty.Index, types.OCIImageIndex),
		mutate.IndexAddendum{Add: amd, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}}},
		mutate.IndexAddendum{Add: arm, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "arm64"}}},
	)
	multiRef, err := name.ParseReference(host+"/apps/multi:latest", name.Insecure)
	must(t, err)
	must(t, remote.WriteIndex(multiRef, idx))
	multiAmd, err := amd.Digest()
	must(t, err)

	st := tg.Open(t)
	logger := zerolog.New(io.Discard)
	x := indexer.New(st, logger, indexer.Options{})

	r, err := st.CreateRegistry(ctx, &store.Registry{Name: "test", URL: reg.URL, AuthType: store.AuthAnonymous, UseCatalog: true})
	must(t, err)
	x.SyncNow(ctx, r.ID)
	r, err = st.GetRegistry(ctx, r.ID)
	must(t, err)
	if r.SyncState != store.SyncOK {
		t.Fatalf("sync state = %s (%s), want ok", r.SyncState, r.LastSyncError)
	}
	// hello (2 digests), annotated, beta, hidden, multi (2 arches), platform,
	// codecs; busybox is no flatpak.
	const imageCount = 9
	if r.ImageCount != imageCount {
		t.Fatalf("image count = %d, want %d", r.ImageCount, imageCount)
	}
	before, err := st.RegistryImages(ctx, r.ID, false)
	must(t, err)

	// A second sync must keep everything (and reuses stored manifests),
	// including the image ids.
	x.SyncNow(ctx, r.ID)
	if r2, _ := st.GetRegistry(ctx, r.ID); r2.ImageCount != imageCount || r2.SyncState != store.SyncOK {
		t.Fatalf("after resync: count=%d state=%s", r2.ImageCount, r2.SyncState)
	}
	after, err := st.RegistryImages(ctx, r.ID, false)
	must(t, err)
	for i := range before {
		if before[i].ID != after[i].ID || before[i].Digest != after[i].Digest {
			t.Errorf("image %s changed id across syncs: %s -> %s", before[i].Ref, before[i].ID, after[i].ID)
		}
	}

	// A second registry (same upstream) providing only the platform runtime.
	other, err := st.CreateRegistry(ctx, &store.Registry{Name: "other", URL: reg.URL, AuthType: store.AuthAnonymous, Repositories: []string{"runtimes/platform"}})
	must(t, err)
	x.SyncNow(ctx, other.ID)
	if o, _ := st.GetRegistry(ctx, other.ID); o.SyncState != store.SyncOK || o.ImageCount != 1 {
		t.Fatalf("other registry: %+v", o)
	}

	srv := httptest.NewServer(server.New(st, x, logger, server.Options{PublicURL: "https://notary.test", IndexMaxAge: time.Minute}))
	defer srv.Close()

	repos := notaryv1connect.NewRepositoryServiceClient(srv.Client(), srv.URL+"/api", connect.WithProtoJSON())
	created, err := repos.CreateRepository(ctx, connect.NewRequest(&notaryv1.CreateRepositoryRequest{Repository: &notaryv1.RepositoryInput{
		Slug:       "test",
		Title:      "Test Repo",
		RegistryId: r.ID,
		Sources: []*notaryv1.Source{
			{RepositoryPattern: "apps/*"},
			{RefPattern: "app/org.example.Hidden/*", Exclude: true},
		},
	}}))
	must(t, err)
	if got := created.Msg.Repository.Urls.Remote; got != "oci+https://notary.test/repo/test" {
		t.Errorf("remote URL = %q", got)
	}
	if got := created.Msg.Repository.ImageCount; got != 6 {
		t.Errorf("repository image count = %d, want 6", got)
	}

	// The index as flatpak requests it.
	body := get(t, srv.URL+"/repo/test/index/static?"+flatpakQuery)
	assertMandatoryKeys(t, body)
	var resp struct {
		Registry string
		Results  []struct {
			Name   string
			Images []struct {
				Digest, MediaType, OS, Architecture string
				Tags                                []string
				Labels                              map[string]string
			}
		}
	}
	must(t, json.Unmarshal(body, &resp))
	if resp.Registry != reg.URL+"/" {
		t.Errorf("Registry = %q, want %q", resp.Registry, reg.URL+"/")
	}
	got := map[string]string{}
	for _, res := range resp.Results {
		for _, img := range res.Images {
			if img.Architecture != "amd64" || img.OS != "linux" || !slices.Contains(img.Tags, "latest") {
				t.Errorf("%s: filter not applied: %+v", res.Name, img)
			}
			if img.Labels["org.flatpak.ref"] == "" || img.Labels["org.flatpak.metadata"] == "" {
				t.Errorf("%s: flatpak labels missing", res.Name)
			}
			got[res.Name] = img.Digest
			// flatpak pulls <Registry>v2/<Name>/manifests/<Digest>.
			u, _ := url.Parse(resp.Registry)
			u = u.JoinPath("v2", res.Name, "manifests", img.Digest)
			req, _ := http.NewRequest(http.MethodHead, u.String(), nil)
			req.Header.Set("Accept", string(types.OCIManifestSchema1))
			if hr, err := http.DefaultClient.Do(req); err != nil || hr.StatusCode != http.StatusOK {
				t.Errorf("manifest %s not pullable from registry: %v %v", u, err, hr.StatusCode)
			}
		}
	}
	want := map[string]string{"apps/hello": helloLatest.String(), "apps/multi": multiAmd.String()}
	if got["apps/hello"] != want["apps/hello"] || got["apps/multi"] != want["apps/multi"] || got["apps/annotated"] == "" {
		t.Errorf("index images = %v, want hello=%s multi=%s and annotated", got, want["apps/hello"], want["apps/multi"])
	}
	if _, ok := got["apps/beta"]; ok {
		t.Error("image without the requested tag served")
	}
	if _, ok := got["apps/hidden"]; ok {
		t.Error("excluded image served")
	}
	if _, ok := got["containers/busybox"]; ok {
		t.Error("non-matching repository served")
	}

	// Without a tag filter, the newest image per ref wins.
	var all struct {
		Results []struct {
			Name   string
			Images []struct{ Digest string }
		}
	}
	must(t, json.Unmarshal(get(t, srv.URL+"/repo/test/index/static"), &all))
	for _, res := range all.Results {
		if res.Name == "apps/hello" && (len(res.Images) != 1 || res.Images[0].Digest != helloLatest.String()) {
			t.Errorf("hello not de-duplicated to newest: %+v", res.Images)
		}
	}

	// Following another tag (remote URL "oci+https://...#beta").
	var beta struct{ Results []struct{ Name string } }
	must(t, json.Unmarshal(get(t, srv.URL+"/repo/test/index/static?"+strings.Replace(flatpakQuery, "tag=latest", "tag=beta", 1)), &beta))
	if len(beta.Results) != 1 || beta.Results[0].Name != "apps/beta" {
		t.Errorf("tag=beta results = %+v, want only apps/beta", beta.Results)
	}

	// A repository matching nothing still serves a valid (empty) index.
	_, err = repos.CreateRepository(ctx, connect.NewRequest(&notaryv1.CreateRepositoryRequest{Repository: &notaryv1.RepositoryInput{
		Slug: "empty", RegistryId: r.ID,
	}}))
	must(t, err)
	if b := string(get(t, srv.URL+"/repo/empty/index/static?"+flatpakQuery)); b != `{"Registry":"`+reg.URL+`/","Results":[]}` {
		t.Errorf("empty index = %s", b)
	}

	// Conditional requests.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/repo/test/index/static?"+flatpakQuery, nil)
	res, err := srv.Client().Do(req)
	must(t, err)
	res.Body.Close()
	req.Header.Set("If-None-Match", res.Header.Get("ETag"))
	res, err = srv.Client().Do(req)
	must(t, err)
	res.Body.Close()
	if res.StatusCode != http.StatusNotModified {
		t.Errorf("conditional GET status = %d, want 304", res.StatusCode)
	}

	if rf := string(get(t, srv.URL+"/repo/test.flatpakrepo")); !strings.Contains(rf, "Url=oci+https://notary.test/repo/test\n") || !strings.Contains(rf, "Title=Test Repo\n") {
		t.Errorf("unexpected .flatpakrepo:\n%s", rf)
	}

	// Preview matches the index and carries appstream metadata.
	pv, err := repos.PreviewRepository(ctx, connect.NewRequest(&notaryv1.PreviewRepositoryRequest{Id: created.Msg.Repository.Id, Architecture: "amd64", Tag: "latest"}))
	must(t, err)
	if len(pv.Msg.Images) != 3 || pv.Msg.TotalSize != 3 {
		t.Errorf("preview has %d images (total %d), want 3", len(pv.Msg.Images), pv.Msg.TotalSize)
	}
	for _, img := range pv.Msg.Images {
		if img.Name != "Hello" || img.Summary != "Says hello" || img.Version != "1.2.3" || !img.HasIcon || img.InstalledSize != 4096 ||
			img.Kind != notaryv1.RefKind_REF_KIND_APP || img.Arch != "x86_64" || img.Branch != "stable" || img.Runtime != platform || img.HasExtraData {
			t.Errorf("preview image metadata: %+v", img)
		}
	}
	// The apps need the platform, which the repository does not serve; the
	// "other" registry has it.
	if len(pv.Msg.MissingRuntimes) != 1 {
		t.Fatalf("missing runtimes: %+v", pv.Msg.MissingRuntimes)
	}
	if m := pv.Msg.MissingRuntimes[0]; m.Runtime != platform || !slices.Equal(m.NeededBy, []string{"org.example.Annotated", "org.example.Hello", "org.example.Multi"}) ||
		len(m.AvailableIn) != 1 || m.AvailableIn[0].Id != other.ID || m.AvailableIn[0].Name != "other" {
		t.Errorf("missing runtime: %+v", m)
	}
	pv, err = repos.PreviewRepository(ctx, connect.NewRequest(&notaryv1.PreviewRepositoryRequest{Id: created.Msg.Repository.Id, Architecture: "amd64", Tag: "latest", PageSize: 2, Offset: 2}))
	must(t, err)
	if len(pv.Msg.Images) != 1 || pv.Msg.TotalSize != 3 || pv.Msg.Images[0].FlatpakId != "org.example.Multi" || len(pv.Msg.MissingRuntimes) != 1 {
		t.Errorf("preview page: %d images, total %d, %d missing runtimes", len(pv.Msg.Images), pv.Msg.TotalSize, len(pv.Msg.MissingRuntimes))
	}
	// A repository serving the runtimes too lacks nothing for amd64; without
	// an architecture filter the aarch64 platform (nowhere indexed) is missing.
	full, err := repos.CreateRepository(ctx, connect.NewRequest(&notaryv1.CreateRepositoryRequest{Repository: &notaryv1.RepositoryInput{
		Slug: "full", RegistryId: r.ID, Sources: []*notaryv1.Source{{RepositoryPattern: "apps/*"}, {RepositoryPattern: "runtimes/*"}},
	}}))
	must(t, err)
	pv, err = repos.PreviewRepository(ctx, connect.NewRequest(&notaryv1.PreviewRepositoryRequest{Id: full.Msg.Repository.Id, Architecture: "amd64", Tag: "latest"}))
	must(t, err)
	if pv.Msg.TotalSize != 6 || len(pv.Msg.MissingRuntimes) != 0 {
		t.Errorf("full preview: total %d, missing %+v", pv.Msg.TotalSize, pv.Msg.MissingRuntimes)
	}
	pv, err = repos.PreviewRepository(ctx, connect.NewRequest(&notaryv1.PreviewRepositoryRequest{Id: full.Msg.Repository.Id, Tag: "latest"}))
	must(t, err)
	if len(pv.Msg.MissingRuntimes) != 1 || pv.Msg.MissingRuntimes[0].Runtime != "org.example.Platform/aarch64/stable" ||
		!slices.Equal(pv.Msg.MissingRuntimes[0].NeededBy, []string{"org.example.Multi"}) || len(pv.Msg.MissingRuntimes[0].AvailableIn) != 0 {
		t.Errorf("full preview without arch filter: missing %+v", pv.Msg.MissingRuntimes)
	}

	// Image listing with pagination and filters.
	images := notaryv1connect.NewImageServiceClient(srv.Client(), srv.URL+"/api")
	li, err := images.ListImages(ctx, connect.NewRequest(&notaryv1.ListImagesRequest{PageSize: 3, Offset: 3}))
	must(t, err)
	if li.Msg.TotalSize != imageCount+1 || len(li.Msg.Images) != 3 {
		t.Errorf("list images page: %d of %d", len(li.Msg.Images), li.Msg.TotalSize)
	}
	li, err = images.ListImages(ctx, connect.NewRequest(&notaryv1.ListImagesRequest{RegistryId: r.ID, Kind: notaryv1.RefKind_REF_KIND_RUNTIME}))
	must(t, err)
	if li.Msg.TotalSize != 2 {
		t.Errorf("list runtime images: %d", li.Msg.TotalSize)
	}
	for _, img := range li.Msg.Images {
		if img.FlatpakId == "org.example.Platform.Codecs" && (!img.HasExtraData || img.Runtime != platform) {
			t.Errorf("codecs image: %+v", img)
		}
		if img.FlatpakId == "org.example.Platform" && (img.HasExtraData || img.Runtime != "") {
			t.Errorf("platform image: %+v", img)
		}
	}
	li, err = images.ListImages(ctx, connect.NewRequest(&notaryv1.ListImagesRequest{FlatpakId: "org.example.Multi", Architecture: "arm64"}))
	must(t, err)
	if li.Msg.TotalSize != 1 || len(li.Msg.Images) != 1 || li.Msg.Images[0].Arch != "aarch64" {
		t.Errorf("list images filtered: %+v", li.Msg)
	}
	li, err = images.ListImages(ctx, connect.NewRequest(&notaryv1.ListImagesRequest{Query: "APPS/HELLO", Kind: notaryv1.RefKind_REF_KIND_APP}))
	must(t, err)
	if li.Msg.TotalSize != 2 {
		t.Errorf("list images query: %d", li.Msg.TotalSize)
	}
	lr, err := images.ListImageRepositories(ctx, connect.NewRequest(&notaryv1.ListImageRepositoriesRequest{RegistryId: r.ID}))
	must(t, err)
	if lr.Msg.TotalSize != 7 || len(lr.Msg.Repositories) != 7 || lr.Msg.Repositories[0].Repository != "apps/annotated" {
		t.Errorf("list image repositories: %+v", lr.Msg)
	}
	var iconImageID string
	for _, ir := range lr.Msg.Repositories {
		if ir.Repository == "apps/multi" && (ir.ImageCount != 2 || !slices.Equal(ir.Architectures, []string{"amd64", "arm64"}) ||
			!slices.Equal(ir.FlatpakIds, []string{"org.example.Multi"}) || ir.IconImageId == "" || ir.Name != "Hello") {
			t.Errorf("multi repository: %+v", ir)
		}
		if ir.Repository == "apps/hello" {
			iconImageID = ir.IconImageId
		}
	}

	// Packages group images by kind and flatpak ID across arches and registries.
	lp, err := images.ListPackages(ctx, connect.NewRequest(&notaryv1.ListPackagesRequest{}))
	must(t, err)
	if lp.Msg.TotalSize != 7 || len(lp.Msg.Packages) != 7 {
		t.Fatalf("list packages: %d/%d", len(lp.Msg.Packages), lp.Msg.TotalSize)
	}
	byID := map[string]*notaryv1.Package{}
	for _, p := range lp.Msg.Packages {
		byID[p.FlatpakId] = p
	}
	if p := byID["org.example.Multi"]; p == nil || p.Kind != notaryv1.RefKind_REF_KIND_APP || p.ImageCount != 2 || p.Name != "Hello" || p.Version != "1.2.3" ||
		!slices.Equal(p.Architectures, []string{"amd64", "arm64"}) || !slices.Equal(p.Branches, []string{"stable"}) || p.IconImageId == "" || p.Updated == nil {
		t.Errorf("multi package: %+v", p)
	}
	if p := byID["org.example.Hello"]; p == nil || p.ImageCount != 2 || len(p.Registries) != 1 || p.Registries[0].Name != "test" {
		t.Errorf("hello package: %+v", p)
	}
	if p := byID["org.example.Platform"]; p == nil || p.Kind != notaryv1.RefKind_REF_KIND_RUNTIME || p.ImageCount != 2 || len(p.Registries) != 2 ||
		p.Registries[0].Name != "other" || p.Registries[1].Id != r.ID || p.HasExtraData {
		t.Errorf("platform package: %+v", p)
	}
	if p := byID["org.example.Platform.Codecs"]; p == nil || !p.HasExtraData {
		t.Errorf("codecs package: %+v", p)
	}
	lp, err = images.ListPackages(ctx, connect.NewRequest(&notaryv1.ListPackagesRequest{RegistryId: other.ID, Architecture: "amd64", Query: "PLATFORM", PageSize: 1}))
	must(t, err)
	if lp.Msg.TotalSize != 1 || len(lp.Msg.Packages) != 1 || lp.Msg.Packages[0].FlatpakId != "org.example.Platform" {
		t.Errorf("filtered packages: %+v", lp.Msg)
	}
	gp, err := images.GetPackage(ctx, connect.NewRequest(&notaryv1.GetPackageRequest{Kind: notaryv1.RefKind_REF_KIND_APP, FlatpakId: "org.example.Multi"}))
	must(t, err)
	if gp.Msg.Package.ImageCount != 2 || len(gp.Msg.Variants) != 2 || gp.Msg.Variants[0].Arch != "x86_64" || gp.Msg.Variants[1].Arch != "aarch64" ||
		gp.Msg.Variants[1].Runtime != "org.example.Platform/aarch64/stable" {
		t.Errorf("get package: %+v", gp.Msg)
	}
	if _, err := images.GetPackage(ctx, connect.NewRequest(&notaryv1.GetPackageRequest{Kind: notaryv1.RefKind_REF_KIND_RUNTIME, FlatpakId: "org.example.Multi"})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("get package of the wrong kind: %v", err)
	}
	if _, err := images.GetPackage(ctx, connect.NewRequest(&notaryv1.GetPackageRequest{FlatpakId: "org.example.Multi"})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("get package without kind: %v", err)
	}

	// Icons are served by image id.
	if iconImageID == "" {
		t.Fatal("no icon image id")
	}
	res, err = srv.Client().Get(srv.URL + "/icons/" + iconImageID)
	must(t, err)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/png" {
		t.Errorf("icon: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	for _, id := range []string{"nope", "12", store.NewID()} {
		res, err := srv.Client().Get(srv.URL + "/icons/" + id)
		must(t, err)
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("icon %s: %d, want 404", id, res.StatusCode)
		}
	}

	// Malformed ids are invalid arguments, well-formed unknown ones not found.
	regs := notaryv1connect.NewRegistryServiceClient(srv.Client(), srv.URL+"/api")
	for name, call := range map[string]func() error{
		"GetRegistry": func() error {
			_, err := regs.GetRegistry(ctx, connect.NewRequest(&notaryv1.GetRegistryRequest{Id: "nope"}))
			return err
		},
		"SyncRegistry": func() error {
			_, err := regs.SyncRegistry(ctx, connect.NewRequest(&notaryv1.SyncRegistryRequest{Id: "12"}))
			return err
		},
		"GetImage": func() error {
			_, err := images.GetImage(ctx, connect.NewRequest(&notaryv1.GetImageRequest{Id: "x"}))
			return err
		},
		"ListImages": func() error {
			_, err := images.ListImages(ctx, connect.NewRequest(&notaryv1.ListImagesRequest{RegistryId: "x"}))
			return err
		},
		"ListPackages": func() error {
			_, err := images.ListPackages(ctx, connect.NewRequest(&notaryv1.ListPackagesRequest{RegistryId: "x"}))
			return err
		},
		"PreviewRepository": func() error {
			_, err := repos.PreviewRepository(ctx, connect.NewRequest(&notaryv1.PreviewRepositoryRequest{Id: "x"}))
			return err
		},
		"CreateRepository": func() error {
			_, err := repos.CreateRepository(ctx, connect.NewRequest(&notaryv1.CreateRepositoryRequest{Repository: &notaryv1.RepositoryInput{Slug: "bad", RegistryId: "x"}}))
			return err
		},
	} {
		if err := call(); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s with a malformed id: %v, want InvalidArgument", name, err)
		}
	}
	if _, err := regs.GetRegistry(ctx, connect.NewRequest(&notaryv1.GetRegistryRequest{Id: store.NewID()})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("unknown registry: %v", err)
	}
	if _, err := images.GetImage(ctx, connect.NewRequest(&notaryv1.GetImageRequest{Id: strings.ToUpper(iconImageID)})); err != nil {
		t.Errorf("uppercase id not accepted: %v", err)
	}
	if _, err := repos.CreateRepository(ctx, connect.NewRequest(&notaryv1.CreateRepositoryRequest{Repository: &notaryv1.RepositoryInput{Slug: "orphan", RegistryId: store.NewID()}})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("repository on an unknown registry: %v", err)
	}
	lr, err = images.ListImageRepositories(ctx, connect.NewRequest(&notaryv1.ListImageRepositoriesRequest{Query: "org.example.hello", PageSize: 1, Offset: 1}))
	must(t, err)
	if lr.Msg.TotalSize != 1 || len(lr.Msg.Repositories) != 0 {
		t.Errorf("list image repositories page past the end: %+v", lr.Msg)
	}

	// A registry in use cannot be deleted.
	_, err = regs.DeleteRegistry(ctx, connect.NewRequest(&notaryv1.DeleteRegistryRequest{Id: r.ID}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("delete used registry: %v, want FailedPrecondition", err)
	}

	// Unknown repositories 404.
	res, err = srv.Client().Get(srv.URL + "/repo/nope/index/static")
	must(t, err)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown repo status = %d", res.StatusCode)
	}
}

// TestExternalSyncer runs the server without an embedded indexer, as with
// "notary serve --sync-enabled=false": it reports so, never claims a sync
// and records sync requests (SyncRegistry, create, update) for the
// separate "notary sync" process, which then finds them due.
func TestExternalSyncer(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		ctx := context.Background()
		st := tg.Open(t)
		logger := zerolog.New(io.Discard)
		srv := httptest.NewServer(server.New(st, nil, logger, server.Options{PublicURL: "https://notary.test"}))
		defer srv.Close()

		system := notaryv1connect.NewSystemServiceClient(srv.Client(), srv.URL+"/api")
		info, err := system.GetInfo(ctx, connect.NewRequest(&notaryv1.GetInfoRequest{}))
		must(t, err)
		if info.Msg.EmbeddedSync {
			t.Error("server without indexer reports embedded sync")
		}

		regs := notaryv1connect.NewRegistryServiceClient(srv.Client(), srv.URL+"/api")
		created, err := regs.CreateRegistry(ctx, connect.NewRequest(&notaryv1.CreateRegistryRequest{Registry: &notaryv1.RegistryInput{
			Name: "ext", Url: "https://registry.example", UseCatalog: true, SyncIntervalMinutes: 0,
		}}))
		must(t, err)
		r := created.Msg.Registry
		if !r.SyncRequested || r.SyncState != notaryv1.SyncState_SYNC_STATE_NEVER {
			t.Errorf("created registry: requested=%v state=%s", r.SyncRequested, r.SyncState)
		}
		// The syncer picks the request up: it is due although manual-only.
		stored, err := st.GetRegistry(ctx, r.Id)
		must(t, err)
		if !indexer.Due(stored) {
			t.Error("requested registry not due")
		}
		claimed, err := st.ClaimSync(ctx, r.Id, "notary-sync", time.Minute)
		must(t, err)
		if !claimed {
			t.Fatal("claim")
		}
		_, err = st.FinishSync(ctx, r.Id, "notary-sync", time.Now(), time.Second, nil)
		must(t, err)
		got, err := regs.GetRegistry(ctx, connect.NewRequest(&notaryv1.GetRegistryRequest{Id: r.Id}))
		must(t, err)
		if got.Msg.Registry.SyncRequested || got.Msg.Registry.SyncState != notaryv1.SyncState_SYNC_STATE_OK {
			t.Errorf("after the syncer ran: %+v", got.Msg.Registry)
		}

		// SyncRegistry only records a request.
		synced, err := regs.SyncRegistry(ctx, connect.NewRequest(&notaryv1.SyncRegistryRequest{Id: r.Id}))
		must(t, err)
		if !synced.Msg.Registry.SyncRequested || synced.Msg.Registry.SyncState != notaryv1.SyncState_SYNC_STATE_OK {
			t.Errorf("after SyncRegistry: %+v", synced.Msg.Registry)
		}
		if stored, _ = st.GetRegistry(ctx, r.Id); stored.SyncOwner != "" || !indexer.Due(stored) {
			t.Errorf("SyncRegistry without indexer touched the lease or is not due: %+v", stored)
		}
		// Progress is reported while syncing, and only then.
		if _, err := st.ClaimSync(ctx, r.Id, "notary-sync", time.Minute); err != nil {
			t.Fatal(err)
		}
		if _, err := st.SetSyncTotal(ctx, r.Id, "notary-sync", 4); err != nil {
			t.Fatal(err)
		}
		must(t, st.WriteSyncBatch(ctx, r.Id, "notary-sync", nil, store.SyncProgress{Done: 1, Total: 4}, time.Minute))
		got, err = regs.GetRegistry(ctx, connect.NewRequest(&notaryv1.GetRegistryRequest{Id: r.Id}))
		must(t, err)
		if p := got.Msg.Registry; p.SyncState != notaryv1.SyncState_SYNC_STATE_SYNCING || p.SyncRequested || p.SyncRepositoriesDone != 1 || p.SyncRepositoriesTotal != 4 {
			t.Errorf("progress while syncing: %+v", p)
		}
		_, err = st.FinishSync(ctx, r.Id, "notary-sync", time.Now(), time.Second, nil)
		must(t, err)
		got, err = regs.GetRegistry(ctx, connect.NewRequest(&notaryv1.GetRegistryRequest{Id: r.Id}))
		must(t, err)
		if p := got.Msg.Registry; p.SyncRepositoriesDone != 0 || p.SyncRepositoriesTotal != 0 {
			t.Errorf("progress reported after the sync: %+v", p)
		}
		// An update requests a sync too; delete works without an indexer.
		updated, err := regs.UpdateRegistry(ctx, connect.NewRequest(&notaryv1.UpdateRegistryRequest{Id: r.Id, Registry: &notaryv1.RegistryInput{
			Name: "ext", Url: "https://registry.example", UseCatalog: true, SyncIntervalMinutes: 30,
		}}))
		must(t, err)
		if !updated.Msg.Registry.SyncRequested {
			t.Error("update did not request a sync")
		}
		if _, err := regs.DeleteRegistry(ctx, connect.NewRequest(&notaryv1.DeleteRegistryRequest{Id: r.Id})); err != nil {
			t.Errorf("delete without indexer: %v", err)
		}
	})
}

func get(t *testing.T, u string) []byte {
	t.Helper()
	res, err := http.Get(u)
	must(t, err)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %s", u, res.Status)
	}
	b, err := io.ReadAll(res.Body)
	must(t, err)
	return b
}

// assertMandatoryKeys checks the fields flatpak's JSON parser requires
// (flatpak-json-oci.c: Registry, Results, Name, Images, Digest, MediaType, OS,
// Architecture are mandatory).
func assertMandatoryKeys(t *testing.T, body []byte) {
	t.Helper()
	var doc map[string]any
	must(t, json.Unmarshal(body, &doc))
	if _, ok := doc["Registry"].(string); !ok {
		t.Fatal("Registry missing")
	}
	results, ok := doc["Results"].([]any)
	if !ok {
		t.Fatal("Results missing or null")
	}
	for _, r := range results {
		r := r.(map[string]any)
		if _, ok := r["Name"].(string); !ok {
			t.Error("Name missing")
		}
		images, ok := r["Images"].([]any)
		if !ok {
			t.Error("Images missing or null")
		}
		for _, i := range images {
			i := i.(map[string]any)
			for _, k := range []string{"Digest", "MediaType", "OS", "Architecture"} {
				if s, ok := i[k].(string); !ok || s == "" {
					t.Errorf("image %s missing", k)
				}
			}
		}
	}
}
