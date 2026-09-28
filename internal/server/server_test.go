package server_test

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
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
)

const flatpakQuery = "label%3Aorg.flatpak.ref%3Aexists=1&architecture=amd64&os=linux&tag=latest"

type imageSpec struct {
	ref, arch string
	created   time.Time
	// annotationsOnly puts the flatpak metadata into manifest annotations
	// instead of config labels.
	annotationsOnly bool
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
		labels = map[string]string{
			"org.flatpak.ref":            s.ref,
			"org.flatpak.metadata":       "[Application]\nname=" + id + "\n",
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
// flatpak index and the admin API.
func TestEndToEnd(t *testing.T) {
	ctx := context.Background()
	reg := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer reg.Close()
	host := strings.TrimPrefix(reg.URL, "http://")

	old := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	helloLatest := push(t, host, "apps/hello:latest", makeImage(t, imageSpec{ref: "app/org.example.Hello/x86_64/stable", arch: "amd64", created: newer}))
	push(t, host, "apps/hello:old", makeImage(t, imageSpec{ref: "app/org.example.Hello/x86_64/stable", arch: "amd64", created: old}))
	push(t, host, "apps/annotated:latest", makeImage(t, imageSpec{ref: "app/org.example.Annotated/x86_64/stable", arch: "amd64", created: newer, annotationsOnly: true}))
	push(t, host, "containers/busybox:latest", makeImage(t, imageSpec{arch: "amd64", created: newer}))
	push(t, host, "apps/beta:beta", makeImage(t, imageSpec{ref: "app/org.example.Beta/x86_64/beta", arch: "amd64", created: newer}))
	push(t, host, "apps/hidden:latest", makeImage(t, imageSpec{ref: "app/org.example.Hidden/x86_64/stable", arch: "amd64", created: newer}))

	amd := makeImage(t, imageSpec{ref: "app/org.example.Multi/x86_64/stable", arch: "amd64", created: newer})
	arm := makeImage(t, imageSpec{ref: "app/org.example.Multi/aarch64/stable", arch: "arm64", created: newer})
	idx := mutate.AppendManifests(mutate.IndexMediaType(empty.Index, types.OCIImageIndex),
		mutate.IndexAddendum{Add: amd, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}}},
		mutate.IndexAddendum{Add: arm, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "arm64"}}},
	)
	multiRef, err := name.ParseReference(host+"/apps/multi:latest", name.Insecure)
	must(t, err)
	must(t, remote.WriteIndex(multiRef, idx))
	multiAmd, err := amd.Digest()
	must(t, err)

	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "notary.db"))
	must(t, err)
	defer st.Close()
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
	// hello (2 digests), annotated, beta, hidden, multi (2 arches); busybox is no flatpak.
	if r.ImageCount != 7 {
		t.Fatalf("image count = %d, want 7", r.ImageCount)
	}

	// A second sync must keep everything (and reuses stored manifests).
	x.SyncNow(ctx, r.ID)
	if r2, _ := st.GetRegistry(ctx, r.ID); r2.ImageCount != 7 || r2.SyncState != store.SyncOK {
		t.Fatalf("after resync: count=%d state=%s", r2.ImageCount, r2.SyncState)
	}

	srv := httptest.NewServer(server.New(st, x, logger, server.Options{PublicURL: "https://notary.test", IndexMaxAge: time.Minute}))
	defer srv.Close()

	repos := notaryv1connect.NewRepositoryServiceClient(srv.Client(), srv.URL+"/api", connect.WithProtoJSON())
	created, err := repos.CreateRepository(ctx, connect.NewRequest(&notaryv1.CreateRepositoryRequest{Repository: &notaryv1.RepositoryInput{
		Slug:       "test",
		Title:      "Test Repo",
		RegistryId: int32(r.ID),
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
		Slug: "empty", RegistryId: int32(r.ID),
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
	if len(pv.Msg.Images) != 3 {
		t.Errorf("preview has %d images, want 3", len(pv.Msg.Images))
	}
	for _, img := range pv.Msg.Images {
		if img.Name != "Hello" || img.Summary != "Says hello" || img.Version != "1.2.3" || !img.HasIcon || img.InstalledSize != 4096 {
			t.Errorf("preview image metadata: %+v", img)
		}
	}

	// A registry in use cannot be deleted.
	regs := notaryv1connect.NewRegistryServiceClient(srv.Client(), srv.URL+"/api")
	_, err = regs.DeleteRegistry(ctx, connect.NewRequest(&notaryv1.DeleteRegistryRequest{Id: int32(r.ID)}))
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
