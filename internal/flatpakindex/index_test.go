package flatpakindex

import (
	"encoding/json"
	"net/url"
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
	img := func(id int64, repo, ref, arch string, created time.Time, tags ...string) *store.Image {
		return &store.Image{ID: id, Repository: repo, Ref: ref, Architecture: arch, OS: "linux", Created: &created, Tags: tags,
			Labels: map[string]string{"org.flatpak.ref": ref}}
	}
	images := []*store.Image{
		img(1, "org/app", "app/a/x86_64/stable", "amd64", t1, "v1"),
		img(2, "org/app", "app/a/x86_64/stable", "amd64", t2, "latest", "v2"),
		img(3, "org/app", "app/a/aarch64/stable", "arm64", t2, "latest"),
		img(4, "org/rt", "runtime/r/x86_64/1", "amd64", t1, "latest"),
		img(5, "other/app", "app/b/x86_64/stable", "amd64", t2, "latest"),
	}
	sources := []store.Source{
		{RepositoryPattern: "org/*"},
		{RefPattern: "runtime/*", Exclude: true},
	}
	ids := func(imgs []*store.Image) (out []int64) {
		for _, i := range imgs {
			out = append(out, i.ID)
		}
		return out
	}
	cases := []struct {
		name string
		f    Filter
		want []int64
	}{
		{"all", Filter{}, []int64{3, 2}},
		{"amd64", Filter{Architectures: []string{"amd64"}}, []int64{2}},
		{"tag v1", Filter{Tags: []string{"v1"}}, []int64{1}},
		{"label", Filter{LabelEquals: map[string]string{"org.flatpak.ref": "app/a/aarch64/stable"}}, []int64{3}},
	}
	for _, c := range cases {
		got := ids(Select(sources, c.f, images))
		if len(got) != len(c.want) || (len(got) > 0 && !equal(got, c.want)) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if len(Select(nil, Filter{}, images)) != 0 {
		t.Error("no sources must select nothing")
	}
}

func equal(a, b []int64) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCacheable(t *testing.T) {
	parse := func(q string) Filter { v, _ := url.ParseQuery(q); return ParseFilter(v) }
	if !cacheable(parse("label%3Aorg.flatpak.ref%3Aexists=1&architecture=amd64&os=linux&tag=latest")) {
		t.Error("flatpak's own query must be cacheable")
	}
	for _, q := range []string{"label%3Afoo=bar", "tag=a&tag=b", "architecture=amd64&architecture=arm64", "label%3Ax%3Aexists=1"} {
		if cacheable(parse(q)) {
			t.Errorf("%s must not be cached", q)
		}
	}
}
