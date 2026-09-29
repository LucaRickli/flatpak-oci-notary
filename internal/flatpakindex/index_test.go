package flatpakindex

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lucarickli/flatpak-oci-notary/internal/store"
)

func TestBuildNeverEmitsNull(t *testing.T) {
	b, err := json.Marshal(Build("https://registry.example", []*store.Image{{
		Repository: "a/b", Digest: "sha256:00", MediaType: "application/vnd.oci.image.manifest.v1+json",
		OS: "linux", Architecture: "amd64", Ref: "app/x/x86_64/stable",
	}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "null") {
		t.Errorf("index contains null: %s", b)
	}
	if !strings.Contains(string(b), `"Registry":"https://registry.example/"`) {
		t.Errorf("registry URL not normalized: %s", b)
	}
}

func TestParseFilter(t *testing.T) {
	q, _ := url.ParseQuery("label%3Aorg.flatpak.ref%3Aexists=1&architecture=amd64&architecture=arm64&os=linux&tag=latest&label%3Afoo=bar")
	f := ParseFilter(q)
	if strings.Join(f.Architectures, ",") != "amd64,arm64" || strings.Join(f.OS, ",") != "linux" ||
		strings.Join(f.Tags, ",") != "latest" || strings.Join(f.LabelExists, ",") != "org.flatpak.ref" || f.LabelEquals["foo"] != "bar" {
		t.Errorf("unexpected filter: %+v", f)
	}
}

func TestSelect(t *testing.T) {
	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	img := func(id string, repo, ref, arch string, created time.Time, tags ...string) *store.Image {
		return &store.Image{ID: id, Repository: repo, Ref: ref, Architecture: arch, OS: "linux", Created: &created, Tags: tags,
			Labels: map[string]string{"org.flatpak.ref": ref}}
	}
	images := []*store.Image{
		img("1", "org/app", "app/a/x86_64/stable", "amd64", t1, "v1"),
		img("2", "org/app", "app/a/x86_64/stable", "amd64", t2, "latest", "v2"),
		img("3", "org/app", "app/a/aarch64/stable", "arm64", t2, "latest"),
		img("4", "org/rt", "runtime/r/x86_64/1", "amd64", t1, "latest"),
		img("5", "other/app", "app/b/x86_64/stable", "amd64", t2, "latest"),
	}
	sources := []store.Source{
		{RepositoryPattern: "org/*"},
		{RefPattern: "runtime/*", Exclude: true},
	}
	ids := func(imgs []*store.Image) (out []string) {
		for _, i := range imgs {
			out = append(out, i.ID)
		}
		return out
	}
	cases := []struct {
		name string
		f    Filter
		want []string
	}{
		{"all", Filter{}, []string{"3", "2"}},
		{"amd64", Filter{Architectures: []string{"amd64"}}, []string{"2"}},
		{"tag v1", Filter{Tags: []string{"v1"}}, []string{"1"}},
		{"label", Filter{LabelEquals: map[string]string{"org.flatpak.ref": "app/a/aarch64/stable"}}, []string{"3"}},
	}
	for _, c := range cases {
		if got := ids(Select(sources, c.f, images)); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if len(Select(nil, Filter{}, images)) != 0 {
		t.Error("no sources must select nothing")
	}
}

func TestMissingRuntimes(t *testing.T) {
	img := func(ref, runtime string) *store.Image {
		parts := strings.Split(ref, "/")
		return &store.Image{Ref: ref, Kind: parts[0], FlatpakID: parts[1], Runtime: runtime}
	}
	extraData := func(i *store.Image) *store.Image { i.HasExtraData = true; return i }
	check := func(name string, got, want []MissingRuntime) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: missing runtimes = %+v, want %+v", name, got, want)
		}
		for i := range want {
			if got[i].Runtime != want[i].Runtime || !slices.Equal(got[i].NeededBy, want[i].NeededBy) {
				t.Errorf("%s: missing[%d] = %+v, want %+v", name, i, got[i], want[i])
			}
		}
	}
	const platform, sdk = "org.example.Platform/x86_64/24.08", "org.example.Sdk/x86_64/24.08"

	check("apps and extra data", MissingRuntimes([]*store.Image{
		img("app/org.b.App/x86_64/stable", platform),
		img("app/org.a.App/x86_64/stable", platform),
		img("app/org.a.App/aarch64/stable", "org.example.Platform/aarch64/24.08"),
		img("app/org.c.App/x86_64/stable", "org.example.Platform/x86_64/23.08"),
		img("runtime/org.example.Platform/x86_64/23.08", ""),
		extraData(img("runtime/org.example.Platform.Codecs/x86_64/24.08", platform)),
		img("app/org.noruntime.App/x86_64/stable", ""),
	}), []MissingRuntime{
		{Runtime: "org.example.Platform/aarch64/24.08", NeededBy: []string{"org.a.App"}},
		{Runtime: platform, NeededBy: []string{"org.a.App", "org.b.App", "org.example.Platform.Codecs"}},
	})
	if len(MissingRuntimes(nil)) != 0 {
		t.Error("empty selection has missing runtimes")
	}

	// SDKs, SDK extensions, locales and plugins name a runtime only for
	// information: flatpak installs them without it.
	sdkImage := img("runtime/org.example.Sdk/x86_64/24.08", platform)
	golang := img("runtime/org.example.Sdk.Extension.golang/x86_64/24.08", sdk)
	locale := img("runtime/org.example.Platform.Locale/x86_64/24.08", platform)
	check("sdk only", MissingRuntimes([]*store.Image{sdkImage}), nil)
	check("sdk extension only", MissingRuntimes([]*store.Image{golang}), nil)
	check("locale only", MissingRuntimes([]*store.Image{locale}), nil)
	check("app and sdk", MissingRuntimes([]*store.Image{sdkImage, golang, img("app/org.a.App/x86_64/stable", platform)}),
		[]MissingRuntime{{Runtime: platform, NeededBy: []string{"org.a.App"}}})
	if NeedsRuntime(sdkImage) || !NeedsRuntime(extraData(img("runtime/org.x.Codecs/x86_64/1", platform))) || NeedsRuntime(img("app/org.x.App/x86_64/stable", "")) {
		t.Error("NeedsRuntime")
	}
}

func TestCacheable(t *testing.T) {
	parse := func(q string) Filter { v, _ := url.ParseQuery(q); return ParseFilter(v) }
	if !cacheable(parse("label%3Aorg.flatpak.ref%3Aexists=1&architecture=amd64&os=linux&tag=latest")) {
		t.Error("flatpak's own query must be cacheable")
	}
	if !cacheable(parse("tag="+strings.Repeat("a", maxFilterValue)+"&architecture=x86_64&os=linux")) || !cacheable(parse("tag=f44.1_rc-2")) {
		t.Error("plausible names must be cacheable")
	}
	for _, q := range []string{"label%3Afoo=bar", "tag=a&tag=b", "architecture=amd64&architecture=arm64", "label%3Ax%3Aexists=1",
		"tag=" + strings.Repeat("a", maxFilterValue+1), "tag=a%20b", "architecture=%00", "os=l%2Finux"} {
		if cacheable(parse(q)) {
			t.Errorf("%s must not be cached", q)
		}
	}
}
