package store_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/lucarickli/flatpak-oci-notary/internal/store"
	"github.com/lucarickli/flatpak-oci-notary/internal/store/storetest"
)

var ctx = context.Background()

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newRegistry(t *testing.T, s *store.Store, name string) *store.Registry {
	t.Helper()
	r, err := s.CreateRegistry(ctx, &store.Registry{Name: name, URL: "https://" + name + ".example", AuthType: store.AuthAnonymous, UseCatalog: true})
	must(t, err)
	return r
}

// img builds an image; tags default to "latest".
func img(repo, ref, arch string, tags ...string) *store.Image {
	if len(tags) == 0 {
		tags = []string{"latest"}
	}
	id := strings.Split(ref, "/")[1]
	return &store.Image{
		Repository: repo, Digest: "sha256:" + fmt.Sprintf("%x", []byte(repo+ref+arch)), MediaType: "application/vnd.oci.image.manifest.v1+json",
		OS: "linux", Architecture: arch, Tags: tags, Ref: ref, Name: "Name " + id, Summary: "Summary of " + id,
		Labels: map[string]string{"org.flatpak.ref": ref}, IndexedAt: time.Now(),
	}
}

func withIcon(i *store.Image) *store.Image {
	i.Labels["org.freedesktop.appstream.icon-64"] = "data:image/png;base64,AAAA"
	return i
}

// withRuntime sets a metadata keyfile declaring the runtime (and, with
// extraData, an [Extra Data] group).
func withRuntime(i *store.Image, runtime string, extraData bool) *store.Image {
	group := "[Application]"
	if strings.HasPrefix(i.Ref, "runtime/") {
		group = "[Runtime]"
	}
	meta := group + "\nname=" + strings.Split(i.Ref, "/")[1] + "\nruntime=" + runtime + "\n"
	if extraData {
		meta += "\n[Extra Data]\nname=blob.bin\nchecksum=00\nsize=1\nuri=https://example.com/blob.bin\n"
	}
	i.Labels["org.flatpak.metadata"] = meta
	return i
}

func withCreated(i *store.Image, t time.Time) *store.Image {
	i.Created = &t
	return i
}

// isV7 reports whether id is a canonical UUIDv7.
func isV7(id string) bool {
	u, err := uuid.Parse(id)
	return err == nil && u.Version() == 7 && u.String() == id
}

func ok(results ...store.RepositoryResult) []store.RepositoryResult { return results }

func repo(name string, images ...*store.Image) store.RepositoryResult {
	return store.RepositoryResult{Repository: name, Images: images}
}

func refs(images []*store.Image) []string {
	out := make([]string, len(images))
	for i, im := range images {
		out[i] = im.Ref + "@" + im.Architecture
	}
	return out
}

func TestRegistries(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		s := tg.Open(t)
		r, err := s.CreateRegistry(ctx, &store.Registry{
			Name: "one", URL: "https://one.example", AuthType: store.AuthBasic, Username: "u", Password: "secret",
			Repositories: []string{"a/b"}, TagPatterns: nil, SyncIntervalMinutes: 15,
		})
		must(t, err)
		if !isV7(r.ID) || r.SyncState != store.SyncNever || r.Password != "secret" || r.TagPatterns == nil || r.RepositoryPatterns == nil {
			t.Fatalf("created registry: %+v", r)
		}
		if r.CreatedAt.IsZero() || r.CreatedAt.Location() != time.UTC {
			t.Errorf("created_at = %v", r.CreatedAt)
		}
		if _, err := s.CreateRegistry(ctx, &store.Registry{Name: "one", URL: "https://x"}); !errors.Is(err, store.ErrConflict) {
			t.Errorf("duplicate name: %v, want ErrConflict", err)
		}
		// A zero sync interval means "manual only" and must be stored as is.
		manual, err := s.CreateRegistry(ctx, &store.Registry{Name: "manual", URL: "https://manual.example", AuthType: store.AuthAnonymous, UseCatalog: true, SyncIntervalMinutes: 0})
		must(t, err)
		if manual.SyncIntervalMinutes != 0 {
			t.Errorf("sync interval 0 stored as %d", manual.SyncIntervalMinutes)
		}
		manual.SyncIntervalMinutes = 0
		if manual, err = s.UpdateRegistry(ctx, manual, false); err != nil || manual.SyncIntervalMinutes != 0 {
			t.Errorf("sync interval 0 after update: %d, %v", manual.SyncIntervalMinutes, err)
		}
		must(t, s.DeleteRegistry(ctx, manual.ID))
		if _, err := s.GetRegistry(ctx, store.NewID()); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("get missing: %v", err)
		}
		if _, err := s.GetRegistry(ctx, "not-a-uuid"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("get malformed: %v", err)
		}

		// Updates keep the password unless asked to set it.
		r.Name, r.Repositories, r.Password = "renamed", []string{"c/d", "e/f"}, "ignored"
		r, err = s.UpdateRegistry(ctx, r, false)
		must(t, err)
		if r.Name != "renamed" || r.Password != "secret" || !slices.Equal(r.Repositories, []string{"c/d", "e/f"}) {
			t.Errorf("after update: %+v", r)
		}
		r.AuthType, r.Password = store.AuthAnonymous, ""
		r, err = s.UpdateRegistry(ctx, r, true)
		must(t, err)
		if r.Password != "" {
			t.Errorf("password not cleared: %q", r.Password)
		}
		two := newRegistry(t, s, "two")
		two.Name = "renamed"
		if _, err := s.UpdateRegistry(ctx, two, false); !errors.Is(err, store.ErrConflict) {
			t.Errorf("rename to existing: %v", err)
		}
		if _, err := s.UpdateRegistry(ctx, &store.Registry{ID: store.NewID(), Name: "x"}, false); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("update missing: %v", err)
		}

		// Counts.
		must(t, s.ReplaceRegistryImages(ctx, r.ID, "", ok(
			repo("a/x", img("a/x", "app/org.x.A/x86_64/stable", "amd64"), img("a/x", "app/org.x.A/aarch64/stable", "arm64")),
			repo("a/y", img("a/y", "app/org.x.B/x86_64/stable", "amd64")),
		)))
		r, err = s.GetRegistry(ctx, r.ID)
		must(t, err)
		if r.ImageCount != 3 || r.RepositoryCount != 2 {
			t.Errorf("counts = %d images, %d repositories", r.ImageCount, r.RepositoryCount)
		}
		regs, err := s.ListRegistries(ctx)
		must(t, err)
		if len(regs) != 2 || regs[0].Name != "renamed" || regs[0].ImageCount != 3 || regs[1].ImageCount != 0 {
			t.Errorf("list: %+v", regs)
		}

		// Deleting a used registry fails; deleting cascades to images.
		_, err = s.CreateRepository(ctx, &store.Repository{Slug: "repo", RegistryID: r.ID})
		must(t, err)
		if err := s.DeleteRegistry(ctx, r.ID); !errors.Is(err, store.ErrConflict) {
			t.Errorf("delete used registry: %v", err)
		}
		must(t, s.ReplaceRegistryImages(ctx, two.ID, "", ok(repo("t/x", img("t/x", "app/org.t.X/x86_64/stable", "amd64")))))
		must(t, s.DeleteRegistry(ctx, two.ID))
		if _, total, err := s.ListImages(ctx, store.ImageFilter{}, store.Page{}); err != nil || total != 3 {
			t.Errorf("images after cascade: %d, %v", total, err)
		}
		if err := s.DeleteRegistry(ctx, two.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("delete twice: %v", err)
		}
	})
}

func TestRepositories(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		s := tg.Open(t)
		r := newRegistry(t, s, "reg")
		p, err := s.CreateRepository(ctx, &store.Repository{Slug: "stable", Title: "Stable", RegistryID: r.ID,
			Sources: []store.Source{{RepositoryPattern: "apps/*"}, {RefPattern: "app/org.hidden.*", Exclude: true}}})
		must(t, err)
		if !isV7(p.ID) || p.RegistryName != "reg" || len(p.Sources) != 2 || !p.Sources[1].Exclude {
			t.Fatalf("created: %+v", p)
		}
		if _, err := s.CreateRepository(ctx, &store.Repository{Slug: "stable", RegistryID: r.ID}); !errors.Is(err, store.ErrConflict) {
			t.Errorf("duplicate slug: %v", err)
		}
		if _, err := s.CreateRepository(ctx, &store.Repository{Slug: "orphan", RegistryID: store.NewID()}); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("unknown registry: %v", err)
		}
		empty, err := s.CreateRepository(ctx, &store.Repository{Slug: "empty", RegistryID: r.ID})
		must(t, err)
		if empty.Sources == nil || len(empty.Sources) != 0 {
			t.Errorf("sources = %#v, want empty slice", empty.Sources)
		}
		p.Slug, p.Sources = "renamed", []store.Source{{RepositoryPattern: "*"}}
		p, err = s.UpdateRepository(ctx, p)
		must(t, err)
		if p.Slug != "renamed" || len(p.Sources) != 1 || p.Sources[0].RepositoryPattern != "*" {
			t.Errorf("after update: %+v", p)
		}
		empty.Slug = "renamed"
		if _, err := s.UpdateRepository(ctx, empty); !errors.Is(err, store.ErrConflict) {
			t.Errorf("rename to existing slug: %v", err)
		}
		if _, err := s.UpdateRepository(ctx, &store.Repository{ID: store.NewID(), Slug: "x", RegistryID: r.ID}); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("update missing: %v", err)
		}
		bySlug, err := s.GetRepositoryBySlug(ctx, "renamed")
		must(t, err)
		if bySlug.ID != p.ID {
			t.Errorf("by slug: %+v", bySlug)
		}
		if _, err := s.GetRepositoryBySlug(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("missing slug: %v", err)
		}
		all, err := s.ListRepositories(ctx)
		must(t, err)
		if len(all) != 2 || all[0].Slug != "empty" || all[1].Slug != "renamed" {
			t.Errorf("list: %+v", all)
		}
		must(t, s.DeleteRepository(ctx, empty.ID))
		if err := s.DeleteRepository(ctx, empty.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("delete twice: %v", err)
		}
	})
}

func TestReplaceRegistryImages(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		s := tg.Open(t)
		r := newRegistry(t, s, "reg")
		created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.FixedZone("x", 3600))
		first := img("a/hello", "app/org.example.Hello/x86_64/stable", "amd64", "latest", "v1")
		first.Created = &created
		must(t, s.ReplaceRegistryImages(ctx, r.ID, "", ok(
			repo("a/hello", first, img("a/hello", "app/org.example.Hello/aarch64/stable", "arm64")),
			repo("a/gone", img("a/gone", "app/org.example.Gone/x86_64/stable", "amd64")),
			repo("a/flaky", withIcon(img("a/flaky", "runtime/org.example.Platform/x86_64/24.08", "amd64"))),
		)))
		all, err := s.RegistryImages(ctx, r.ID, true)
		must(t, err)
		if len(all) != 4 {
			t.Fatalf("got %d images", len(all))
		}
		ids := map[string]bool{}
		for _, im := range all {
			if !isV7(im.ID) || ids[im.ID] {
				t.Errorf("image id %q is not a fresh UUIDv7", im.ID)
			}
			ids[im.ID] = true
		}
		hello := all[2] // ordered by ref: Gone, Hello/aarch64, Hello/x86_64, runtime
		if hello.Ref != "app/org.example.Hello/x86_64/stable" || hello.Kind != "app" || hello.FlatpakID != "org.example.Hello" ||
			hello.Arch != "x86_64" || hello.Branch != "stable" || hello.RegistryName != "reg" || hello.HasIcon ||
			!slices.Equal(hello.Tags, []string{"latest", "v1"}) || hello.Labels["org.flatpak.ref"] == "" {
			t.Errorf("hello: %+v", hello)
		}
		if hello.Created == nil || !hello.Created.Equal(created) || hello.Created.Location() != time.UTC {
			t.Errorf("created = %v", hello.Created)
		}
		if rt := all[3]; rt.Kind != "runtime" || !rt.HasIcon || rt.FlatpakID != "org.example.Platform" {
			t.Errorf("runtime: %+v", rt)
		}
		helloID := hello.ID

		// Second sync: hello keeps the amd64 image only (same digest keeps its
		// id, tags refreshed), flaky failed (kept), gone is absent (removed),
		// new appears.
		first.Tags = []string{"latest", "v2"}
		must(t, s.ReplaceRegistryImages(ctx, r.ID, "", ok(
			repo("a/hello", first),
			store.RepositoryResult{Repository: "a/flaky", Err: errors.New("boom")},
			repo("a/new", img("a/new", "app/org.example.New/x86_64/stable", "amd64")),
		)))
		all, err = s.RegistryImages(ctx, r.ID, false)
		must(t, err)
		want := []string{"app/org.example.Hello/x86_64/stable@amd64", "app/org.example.New/x86_64/stable@amd64", "runtime/org.example.Platform/x86_64/24.08@amd64"}
		if got := refs(all); !slices.Equal(got, want) {
			t.Errorf("after second sync: %v, want %v", got, want)
		}
		if all[0].ID != helloID || !slices.Equal(all[0].Tags, []string{"latest", "v2"}) || all[0].Labels != nil {
			t.Errorf("hello after upsert: %+v", all[0])
		}
		got, err := s.GetImage(ctx, helloID)
		must(t, err)
		if got.Labels["org.flatpak.ref"] != hello.Ref {
			t.Errorf("labels not loaded: %+v", got)
		}
		if _, err := s.GetImage(ctx, store.NewID()); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("missing image: %v", err)
		}
		// A third sync with different metadata refreshes the derived
		// columns but keeps the id.
		first.Labels["org.flatpak.metadata"] = "[Application]\nname=org.example.Hello\nruntime=org.example.Platform/x86_64/24.08\n"
		must(t, s.ReplaceRegistryImages(ctx, r.ID, "", ok(
			repo("a/hello", first),
			store.RepositoryResult{Repository: "a/flaky", Err: errors.New("boom")},
			repo("a/new", img("a/new", "app/org.example.New/x86_64/stable", "amd64")),
		)))
		got, err = s.GetImage(ctx, helloID)
		must(t, err)
		if got.Runtime != "org.example.Platform/x86_64/24.08" || got.RuntimeRef() != "runtime/org.example.Platform/x86_64/24.08" {
			t.Errorf("runtime not re-derived on resync: %+v", got)
		}

		// A stale owner cannot write.
		if err := s.ReplaceRegistryImages(ctx, r.ID, "nobody", ok(repo("a/x"))); !errors.Is(err, store.ErrLeaseLost) {
			t.Errorf("write without lease: %v", err)
		}
		if all, _ = s.RegistryImages(ctx, r.ID, false); len(all) != 3 {
			t.Errorf("images changed by a writer without lease: %d", len(all))
		}
	})
}

func TestListImages(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		s := tg.Open(t)
		r1, r2 := newRegistry(t, s, "r1"), newRegistry(t, s, "r2")
		var results []store.RepositoryResult
		for _, n := range []string{"aaa", "bbb", "ccc", "ddd", "eee"} {
			results = append(results, repo("apps/"+n,
				img("apps/"+n, "app/org.test."+n+"/x86_64/stable", "amd64"),
				img("apps/"+n, "app/org.test."+n+"/aarch64/stable", "arm64")))
		}
		results = append(results, repo("runtimes/platform", img("runtimes/platform", "runtime/org.test.Platform/x86_64/1", "amd64")))
		meta := img("apps/meta", "app/org.test.meta/x86_64/stable", "amd64")
		meta.Name = "a_b"
		results = append(results, repo("apps/meta", meta))
		must(t, s.ReplaceRegistryImages(ctx, r1.ID, "", results))
		must(t, s.ReplaceRegistryImages(ctx, r2.ID, "", ok(
			repo("other/pct", func() *store.Image {
				i := img("other/pct", "app/org.other.pct/x86_64/stable", "amd64")
				i.Name = "a%b"
				return i
			}()),
			repo("other/bsl", func() *store.Image {
				i := img("other/bsl", "app/org.other.bsl/x86_64/stable", "amd64")
				i.Name = `a\b`
				return i
			}()),
			repo("other/case", func() *store.Image {
				i := img("other/case", "app/org.other.upper/x86_64/stable", "amd64")
				i.Name = "AxB"
				return i
			}()),
		)))
		const all = 15

		// Default page.
		imgs, total, err := s.ListImages(ctx, store.ImageFilter{}, store.Page{})
		must(t, err)
		if total != all || len(imgs) != all {
			t.Fatalf("all: %d/%d", len(imgs), total)
		}
		if imgs[0].Ref != "app/org.other.bsl/x86_64/stable" || imgs[all-1].Ref != "runtime/org.test.Platform/x86_64/1" {
			t.Errorf("order: first %s, last %s", imgs[0].Ref, imgs[all-1].Ref)
		}
		for i := 1; i < len(imgs); i++ {
			if imgs[i].Ref < imgs[i-1].Ref && !strings.EqualFold(imgs[i].Ref, imgs[i-1].Ref) {
				t.Errorf("not ordered: %s before %s", imgs[i-1].Ref, imgs[i].Ref)
			}
		}
		if imgs[0].Labels != nil {
			t.Error("labels loaded without WithLabels")
		}

		// Paging: limits, offsets, total, disjoint pages covering everything.
		var seen []string
		for off := 0; off < all; off += 4 {
			page, total, err := s.ListImages(ctx, store.ImageFilter{}, store.Page{Size: 4, Offset: off})
			must(t, err)
			if total != all || len(page) != min(4, all-off) {
				t.Errorf("page offset %d: %d items, total %d", off, len(page), total)
			}
			for _, im := range page {
				if slices.Contains(seen, im.ID) {
					t.Errorf("image %s on two pages", im.ID)
				}
				seen = append(seen, im.ID)
			}
		}
		if len(seen) != all {
			t.Errorf("pages cover %d images", len(seen))
		}
		if page, _, _ := s.ListImages(ctx, store.ImageFilter{}, store.Page{Size: 4, Offset: 100}); len(page) != 0 || page == nil {
			t.Errorf("offset past the end: %v", page)
		}
		if page, _, _ := s.ListImages(ctx, store.ImageFilter{}, store.Page{Size: 1000, Offset: -5}); len(page) != all {
			t.Errorf("clamped page: %d", len(page))
		}
		if page, _, _ := s.ListImages(ctx, store.ImageFilter{RegistryID: r1.ID}, store.Page{Size: 3}); len(page) != 3 {
			t.Errorf("registry page: %d", len(page))
		}

		// Filters.
		cases := []struct {
			name string
			f    store.ImageFilter
			want int
		}{
			{"registry", store.ImageFilter{RegistryID: r1.ID}, 12},
			{"kind app", store.ImageFilter{Kind: store.KindApp}, 14},
			{"kind runtime", store.ImageFilter{Kind: store.KindRuntime}, 1},
			{"flatpak id", store.ImageFilter{FlatpakID: "org.test.aaa"}, 2},
			{"flatpak id + arch", store.ImageFilter{FlatpakID: "org.test.aaa", Architecture: "arm64"}, 1},
			{"arch", store.ImageFilter{Architecture: "arm64"}, 5},
			{"query ref", store.ImageFilter{Query: "org.test.b"}, 2},
			{"query case-insensitive", store.ImageFilter{Query: "ORG.TEST.B"}, 2},
			{"query repository", store.ImageFilter{Query: "runtimes/"}, 1},
			{"query summary", store.ImageFilter{Query: "summary of org.test.ccc"}, 2},
			{"query name case", store.ImageFilter{Query: "axb"}, 1},
			{"query underscore literal", store.ImageFilter{Query: "a_b"}, 1},
			{"query percent literal", store.ImageFilter{Query: "a%b"}, 1},
			{"query backslash literal", store.ImageFilter{Query: `a\b`}, 1},
			{"query no match", store.ImageFilter{Query: "zzz"}, 0},
			{"combined", store.ImageFilter{RegistryID: r2.ID, Query: "other", Architecture: "amd64", Kind: store.KindApp}, 3},
			{"combined mismatch", store.ImageFilter{RegistryID: r2.ID, FlatpakID: "org.test.aaa"}, 0},
		}
		for _, c := range cases {
			imgs, total, err := s.ListImages(ctx, c.f, store.Page{})
			must(t, err)
			if total != c.want || len(imgs) != c.want {
				t.Errorf("%s: %d/%d, want %d", c.name, len(imgs), total, c.want)
			}
		}
		withLabels, _, err := s.ListImages(ctx, store.ImageFilter{FlatpakID: "org.test.aaa", WithLabels: true}, store.Page{})
		must(t, err)
		if withLabels[0].Labels["org.flatpak.ref"] == "" {
			t.Error("WithLabels did not load labels")
		}
	})
}

func TestListImageRepositories(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		s := tg.Open(t)
		r1, r2 := newRegistry(t, s, "r1"), newRegistry(t, s, "r2")
		must(t, s.ReplaceRegistryImages(ctx, r1.ID, "", ok(
			repo("apps/hello",
				img("apps/hello", "app/org.example.Hello/x86_64/stable", "amd64"),
				img("apps/hello", "app/org.example.Hello/aarch64/stable", "arm64"),
				withIcon(img("apps/hello", "app/org.example.Hello/x86_64/beta", "amd64", "beta"))),
			repo("apps/multi",
				img("apps/multi", "app/org.example.Multi/x86_64/stable", "amd64"),
				img("apps/multi", "app/org.example.Multi.Extra/x86_64/stable", "amd64")),
			repo("runtimes/platform", img("runtimes/platform", "runtime/org.example.Platform/x86_64/1", "amd64")),
		)))
		must(t, s.ReplaceRegistryImages(ctx, r2.ID, "", ok(
			repo("apps/hello", img("apps/hello", "app/com.other.Hello/x86_64/stable", "amd64")),
			repo("zzz/last", img("zzz/last", "app/com.other.Last/x86_64/stable", "amd64")),
		)))

		repos, total, err := s.ListImageRepositories(ctx, store.ImageRepositoryFilter{}, store.Page{})
		must(t, err)
		if total != 5 || len(repos) != 5 {
			t.Fatalf("all: %d/%d", len(repos), total)
		}
		names := func(rs []*store.ImageRepository) (out []string) {
			for _, r := range rs {
				out = append(out, fmt.Sprintf("%s@%s", r.Repository, r.RegistryName))
			}
			return out
		}
		want := []string{"apps/hello@r1", "apps/hello@r2", "apps/multi@r1", "runtimes/platform@r1", "zzz/last@r2"}
		if got := names(repos); !slices.Equal(got, want) {
			t.Errorf("order: %v, want %v", got, want)
		}
		hello := repos[0]
		if hello.RegistryID != r1.ID || hello.ImageCount != 3 || hello.Kind != "app" || hello.Name != "Name org.example.Hello" ||
			!slices.Equal(hello.FlatpakIDs, []string{"org.example.Hello"}) || !slices.Equal(hello.Architectures, []string{"amd64", "arm64"}) {
			t.Errorf("hello: %+v", hello)
		}
		if hello.IconImageID == "" {
			t.Error("icon image not found")
		} else if im, err := s.GetImage(ctx, hello.IconImageID); err != nil || !im.HasIcon || im.Repository != "apps/hello" {
			t.Errorf("icon image %s: %+v %v", hello.IconImageID, im, err)
		}
		if multi := repos[2]; !slices.Equal(multi.FlatpakIDs, []string{"org.example.Multi", "org.example.Multi.Extra"}) || multi.IconImageID != "" {
			t.Errorf("multi: %+v", multi)
		}
		if rt := repos[3]; rt.Kind != "runtime" || rt.ImageCount != 1 {
			t.Errorf("runtime: %+v", rt)
		}

		// Paging.
		page, total, err := s.ListImageRepositories(ctx, store.ImageRepositoryFilter{}, store.Page{Size: 2, Offset: 1})
		must(t, err)
		if total != 5 || !slices.Equal(names(page), want[1:3]) {
			t.Errorf("page: %v total %d", names(page), total)
		}
		if page, _, _ := s.ListImageRepositories(ctx, store.ImageRepositoryFilter{}, store.Page{Size: 2, Offset: 10}); len(page) != 0 || page == nil {
			t.Errorf("past the end: %v", page)
		}

		// Filters: a repository matches when any image matches, but the
		// summary always covers all its images.
		cases := []struct {
			name string
			f    store.ImageRepositoryFilter
			want []string
		}{
			{"registry", store.ImageRepositoryFilter{RegistryID: r2.ID}, []string{"apps/hello@r2", "zzz/last@r2"}},
			{"repository", store.ImageRepositoryFilter{Query: "APPS/"}, []string{"apps/hello@r1", "apps/hello@r2", "apps/multi@r1"}},
			{"flatpak id", store.ImageRepositoryFilter{Query: "multi.extra"}, []string{"apps/multi@r1"}},
			{"name", store.ImageRepositoryFilter{Query: "name com.other.last"}, []string{"zzz/last@r2"}},
			{"registry + query", store.ImageRepositoryFilter{RegistryID: r1.ID, Query: "hello"}, []string{"apps/hello@r1"}},
			{"none", store.ImageRepositoryFilter{Query: "nothing_here"}, nil},
		}
		for _, c := range cases {
			got, total, err := s.ListImageRepositories(ctx, c.f, store.Page{})
			must(t, err)
			if total != len(c.want) || !slices.Equal(names(got), c.want) {
				t.Errorf("%s: %v (total %d), want %v", c.name, names(got), total, c.want)
			}
		}
		partial, _, _ := s.ListImageRepositories(ctx, store.ImageRepositoryFilter{Query: "multi.extra"}, store.Page{})
		if partial[0].ImageCount != 2 {
			t.Errorf("filtered repository summary covers %d images, want 2", partial[0].ImageCount)
		}
	})
}

func TestSyncLease(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		a, b := tg.Open(t), tg.Open(t)
		r := newRegistry(t, a, "reg")
		const lease = time.Minute

		claimed, err := a.ClaimSync(ctx, r.ID, "alpha", lease)
		must(t, err)
		if !claimed {
			t.Fatal("first claim failed")
		}
		if claimed, _ := b.ClaimSync(ctx, r.ID, "beta", lease); claimed {
			t.Fatal("second owner claimed a held lease")
		}
		if claimed, _ := a.ClaimSync(ctx, r.ID, "alpha", lease); claimed {
			t.Fatal("re-claim by the owner while running must fail too")
		}
		got, _ := b.GetRegistry(ctx, r.ID)
		if got.SyncState != store.SyncSyncing || got.SyncOwner != "alpha" || got.LeaseExpired {
			t.Errorf("state while syncing: %+v", got)
		}
		// The lease end is computed with the database clock.
		if d := time.Until(time.UnixMilli(got.SyncLeaseUntil)); d < 50*time.Second || d > 70*time.Second {
			t.Errorf("lease ends in %v, want about %v", d, lease)
		}
		if renewed, _ := b.RenewSyncLease(ctx, r.ID, "beta", lease); renewed {
			t.Error("non-owner renewed the lease")
		}
		if renewed, _ := a.RenewSyncLease(ctx, r.ID, "alpha", 2*lease); !renewed {
			t.Error("owner could not renew")
		}
		if done, _ := b.FinishSync(ctx, r.ID, "beta", time.Now(), time.Second, nil); done {
			t.Error("non-owner finished the sync")
		}
		if claimed, _ := b.ClaimSync(ctx, r.ID, "beta", lease); claimed {
			t.Fatal("claimed while the lease is still valid")
		}
		must(t, a.ReplaceRegistryImages(ctx, r.ID, "alpha", ok(repo("a/x", img("a/x", "app/org.x.A/x86_64/stable", "amd64")))))
		if err := b.ReplaceRegistryImages(ctx, r.ID, "beta", ok(repo("a/x"))); !errors.Is(err, store.ErrLeaseLost) {
			t.Errorf("non-owner wrote images: %v", err)
		}
		done, err := a.FinishSync(ctx, r.ID, "alpha", time.Now(), 1500*time.Millisecond, nil)
		must(t, err)
		if !done {
			t.Fatal("owner could not finish")
		}
		got, _ = b.GetRegistry(ctx, r.ID)
		if got.SyncState != store.SyncOK || got.SyncOwner != "" || got.SyncLeaseUntil != 0 || got.LastSyncAt == nil ||
			got.LastSyncDurationMs != 1500 || got.ImageCount != 1 {
			t.Errorf("after finish: %+v", got)
		}
		if done, _ := a.FinishSync(ctx, r.ID, "alpha", time.Now(), time.Second, nil); done {
			t.Error("finish twice")
		}

		// An expired lease is shown as interrupted and can be claimed again.
		claimed, err = a.ClaimSync(ctx, r.ID, "alpha", -time.Second)
		must(t, err)
		if !claimed {
			t.Fatal("claim after finish failed")
		}
		got, _ = b.GetRegistry(ctx, r.ID)
		if got.SyncState != store.SyncError || got.LastSyncError != store.LeaseExpiredError {
			t.Errorf("expired lease shown as: %s %q", got.SyncState, got.LastSyncError)
		}
		if renewed, _ := a.RenewSyncLease(ctx, r.ID, "alpha", lease); !renewed {
			// The raw row is still "syncing" by alpha, so alpha may still renew
			// until someone else claims it.
			t.Error("owner could not renew an expired but unclaimed lease")
		}
		must(t, a.DB().Model(&store.Registry{}).Where("id = ?", r.ID).UpdateColumn("sync_lease_until", time.Now().Add(-time.Second).UnixMilli()).Error)
		claimed, err = b.ClaimSync(ctx, r.ID, "beta", lease)
		must(t, err)
		if !claimed {
			t.Fatal("expired lease not claimable")
		}
		if renewed, _ := a.RenewSyncLease(ctx, r.ID, "alpha", lease); renewed {
			t.Error("previous owner renewed after losing the lease")
		}
		if done, _ := a.FinishSync(ctx, r.ID, "alpha", time.Now(), time.Second, errors.New("late")); done {
			t.Error("previous owner finished after losing the lease")
		}
		done, err = b.FinishSync(ctx, r.ID, "beta", time.Now(), time.Second, errors.New("failed"))
		must(t, err)
		if !done {
			t.Fatal("new owner could not finish")
		}
		got, _ = a.GetRegistry(ctx, r.ID)
		if got.SyncState != store.SyncError || got.LastSyncError != "failed" {
			t.Errorf("after failed finish: %+v", got)
		}
		if claimed, _ := b.ClaimSync(ctx, store.NewID(), "beta", lease); claimed {
			t.Error("claimed a missing registry")
		}
	})
}

func TestGenerationSharedAcrossStores(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		a, b := tg.Open(t), tg.Open(t)
		g0, err := b.Generation(ctx)
		must(t, err)
		r := newRegistry(t, a, "reg")
		next := func(what string) uint64 {
			t.Helper()
			g, err := b.Generation(ctx)
			must(t, err)
			if g <= g0 {
				t.Errorf("%s: generation %d did not advance past %d", what, g, g0)
			}
			g0 = g
			return g
		}
		p, err := a.CreateRepository(ctx, &store.Repository{Slug: "s", RegistryID: r.ID})
		must(t, err)
		next("create repository")
		must(t, a.ReplaceRegistryImages(ctx, r.ID, "", ok(repo("a/x", img("a/x", "app/org.x.A/x86_64/stable", "amd64")))))
		next("replace images")
		_, err = a.UpdateRepository(ctx, p)
		must(t, err)
		next("update repository")
		_, err = a.UpdateRegistry(ctx, r, false)
		must(t, err)
		next("update registry")
		must(t, a.DeleteRepository(ctx, p.ID))
		next("delete repository")
		must(t, a.DeleteRegistry(ctx, r.ID))
		next("delete registry")
		// Sync bookkeeping does not invalidate indexes.
		r2 := newRegistry(t, a, "reg2")
		ga, _ := a.Generation(ctx)
		_, _ = a.ClaimSync(ctx, r2.ID, "o", time.Minute)
		_, _ = a.FinishSync(ctx, r2.ID, "o", time.Now(), 0, nil)
		if g, _ := b.Generation(ctx); g != ga {
			t.Errorf("sync bookkeeping changed the generation: %d -> %d", ga, g)
		}
		// A failed write does not bump.
		if _, err := a.CreateRepository(ctx, &store.Repository{Slug: "x", RegistryID: store.NewID()}); err == nil {
			t.Fatal("expected failure")
		}
		if g, _ := b.Generation(ctx); g != ga {
			t.Errorf("failed write changed the generation")
		}
	})
}

func TestOverview(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		s := tg.Open(t)
		r := newRegistry(t, s, "reg")
		_, err := s.CreateRepository(ctx, &store.Repository{Slug: "s", RegistryID: r.ID})
		must(t, err)
		must(t, s.ReplaceRegistryImages(ctx, r.ID, "", ok(
			repo("a/x", img("a/x", "app/org.x.A/x86_64/stable", "amd64"), img("a/x", "app/org.x.A/aarch64/stable", "arm64"), img("a/x", "app/org.x.A/x86_64/beta", "amd64", "beta")),
			repo("a/y", img("a/y", "app/org.x.B/x86_64/stable", "amd64")),
			repo("r/p", img("r/p", "runtime/org.x.Platform/x86_64/1", "amd64"), img("r/p", "runtime/org.x.Platform/x86_64/2", "amd64", "two")),
		)))
		o, err := s.Overview(ctx)
		must(t, err)
		if o != (store.Overview{Registries: 1, Repositories: 1, Images: 6, Apps: 2, Runtimes: 1}) {
			t.Errorf("overview: %+v", o)
		}
	})
}

func TestPageNormalize(t *testing.T) {
	for in, want := range map[store.Page]store.Page{
		{}:                        {Size: 50},
		{Size: 10, Offset: 5}:     {Size: 10, Offset: 5},
		{Size: 9999, Offset: -1}:  {Size: 500},
		{Size: -3, Offset: 7}:     {Size: 50, Offset: 7},
		{Size: 500, Offset: 1000}: {Size: 500, Offset: 1000},
	} {
		if got := in.Normalize(); got != want {
			t.Errorf("%+v: got %+v, want %+v", in, got, want)
		}
	}
	items := []int{1, 2, 3, 4, 5}
	if got := store.Slice(items, store.Page{Size: 2, Offset: 3}); !slices.Equal(got, []int{4, 5}) {
		t.Errorf("slice: %v", got)
	}
	if got := store.Slice(items, store.Page{Size: 2, Offset: 9}); got == nil || len(got) != 0 {
		t.Errorf("slice past end: %#v", got)
	}
}

func TestOldDatabaseRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	s, err := store.Open(ctx, store.Config{DSN: path, Logger: zerolog.Nop()})
	must(t, err)
	must(t, s.DB().Exec("CREATE TABLE schema_version (version INTEGER NOT NULL)").Error)
	must(t, s.DB().Exec("DROP TABLE migrations").Error)
	must(t, s.Close())
	if _, err := store.Open(ctx, store.Config{DSN: path, Logger: zerolog.Nop()}); err == nil || !strings.Contains(err.Error(), "older") || !strings.Contains(err.Error(), "delete it") {
		t.Errorf("old database accepted: %v", err)
	}
	// Databases created by superseded initial migrations (integer ids; no
	// sync request and progress columns).
	for _, id := range []string{"20260928_initial", "20260929_initial"} {
		prev := filepath.Join(t.TempDir(), "prev.db")
		s, err = store.Open(ctx, store.Config{DSN: prev, Logger: zerolog.Nop()})
		must(t, err)
		must(t, s.DB().Exec("UPDATE migrations SET id = ?", id).Error)
		must(t, s.Close())
		if _, err := store.Open(ctx, store.Config{DSN: prev, Logger: zerolog.Nop()}); err == nil || !strings.Contains(err.Error(), "delete it") {
			t.Errorf("database of previous development version %s accepted: %v", id, err)
		}
	}
	// Reopening a current database is fine.
	s, err = store.Open(ctx, store.Config{DSN: path + ".ok", Logger: zerolog.Nop()})
	must(t, err)
	must(t, s.Close())
	s, err = store.Open(ctx, store.Config{DSN: path + ".ok", Logger: zerolog.Nop()})
	must(t, err)
	must(t, s.Close())
	if _, err := store.Open(ctx, store.Config{Driver: "mysql", DSN: "x"}); err == nil {
		t.Error("unknown driver accepted")
	}
	if _, err := store.Open(ctx, store.Config{Driver: store.DriverPostgres}); err == nil {
		t.Error("postgres without DSN accepted")
	}
}

func TestRedacted(t *testing.T) {
	t.Setenv("PGUSER", "envuser")
	t.Setenv("PGPASSWORD", "envsecret")
	for in, want := range map[string]string{
		"postgres://notary:secret@db:5432/notary?sslmode=disable":             "postgres://notary@db:5432/notary",
		"postgres://notary@db:5432/notary?password=secret&sslmode=require":    "postgres://notary@db:5432/notary",
		"host=db user=notary password=secret dbname=notary":                   "postgres://notary@db:5432/notary",
		"host=db user=notary password='my secret' dbname=notary port=6543":    "postgres://notary@db:6543/notary",
		`host=db user=notary password='it\'s secret' dbname=notary`:           "postgres://notary@db:5432/notary",
		"postgres:///notary?host=/run/postgresql&user=notary&password=secret": "postgres://notary@/notary?host=%2Frun%2Fpostgresql&port=5432",
		"postgres://db/notary":                        "postgres://envuser@db:5432/notary",
		"postgres://notary:secret@db:notaport/notary": "postgres (unparseable dsn)",
	} {
		got := (store.Config{Driver: store.DriverPostgres, DSN: in}).Redacted()
		if got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
		if strings.Contains(got, "secret") {
			t.Errorf("%s: password leaked: %s", in, got)
		}
	}
	if got := (store.Config{Driver: store.DriverSQLite, DSN: "/data/notary.db"}).Redacted(); got != "/data/notary.db" {
		t.Errorf("sqlite: %s", got)
	}
}

func TestMetadataColumns(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		s := tg.Open(t)
		r := newRegistry(t, s, "reg")
		app := withRuntime(img("apps/app", "app/org.example.App/x86_64/stable", "amd64"), "org.example.Platform/x86_64/24.08", false)
		codec := withRuntime(img("rt/codec", "runtime/org.example.Codecs/x86_64/24.08", "amd64"), "org.example.Platform/x86_64/24.08", true)
		ext := img("apps/ext", "runtime/org.example.App.Plugin/x86_64/stable", "amd64")
		ext.Labels["org.flatpak.metadata"] = "[Runtime]\nname=org.example.App.Plugin\nruntime=org.example.Platform/x86_64/24.08\n[ExtensionOf]\nref=app/org.example.App/x86_64/stable\n"
		plain := img("rt/platform", "runtime/org.example.Platform/x86_64/24.08", "amd64")
		must(t, s.ReplaceRegistryImages(ctx, r.ID, "", ok(repo("apps/app", app), repo("rt/codec", codec), repo("apps/ext", ext), repo("rt/platform", plain))))
		all, err := s.RegistryImages(ctx, r.ID, false)
		must(t, err)
		byRef := map[string]*store.Image{}
		for _, im := range all {
			byRef[im.Ref] = im
		}
		if a := byRef[app.Ref]; a.Runtime != "org.example.Platform/x86_64/24.08" || a.ExtensionOf != "" || a.HasExtraData {
			t.Errorf("app: %+v", a)
		}
		if c := byRef[codec.Ref]; c.Runtime != "org.example.Platform/x86_64/24.08" || !c.HasExtraData {
			t.Errorf("codec: %+v", c)
		}
		if e := byRef[ext.Ref]; e.ExtensionOf != "app/org.example.App/x86_64/stable" || e.Runtime == "" {
			t.Errorf("extension: %+v", e)
		}
		if p := byRef[plain.Ref]; p.Runtime != "" || p.ExtensionOf != "" || p.HasExtraData || p.RuntimeRef() != "" {
			t.Errorf("plain runtime: %+v", p)
		}
	})
}

func TestPackages(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		s := tg.Open(t)
		r1, r2 := newRegistry(t, s, "r1"), newRegistry(t, s, "r2")
		t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		t2, t3 := t1.Add(24*time.Hour), t1.Add(48*time.Hour)
		const platform = "org.example.Platform/x86_64/24.08"
		beta := withIcon(withCreated(withRuntime(img("apps/hello", "app/org.example.Hello/x86_64/beta", "amd64", "beta"), platform, false), t3))
		beta.Name, beta.Version, beta.Summary = "Hello Beta", "2.0", "Greets, soon"
		zed := img("apps/zed", "app/org.example.Zed/x86_64/stable", "amd64")
		zed.Name = ""
		must(t, s.ReplaceRegistryImages(ctx, r1.ID, "", ok(
			repo("apps/hello",
				withCreated(withRuntime(img("apps/hello", "app/org.example.Hello/x86_64/stable", "amd64"), platform, false), t2),
				withCreated(withRuntime(img("apps/hello", "app/org.example.Hello/aarch64/stable", "arm64"), "org.example.Platform/aarch64/24.08", false), t2),
				beta),
			repo("apps/zed", zed),
			repo("rt/platform",
				img("rt/platform", "runtime/org.example.Platform/x86_64/24.08", "amd64"),
				img("rt/platform", "runtime/org.example.Platform/aarch64/24.08", "arm64")),
			repo("rt/codecs", withRuntime(img("rt/codecs", "runtime/org.example.Codecs/x86_64/24.08", "amd64"), platform, true)),
			repo("rt/hello", img("rt/hello", "runtime/org.example.Hello/x86_64/24.08", "amd64")),
		)))
		alpha := img("other/alpha", "app/org.other.Alpha/x86_64/stable", "amd64")
		alpha.Name = "alpha app"
		must(t, s.ReplaceRegistryImages(ctx, r2.ID, "", ok(
			repo("mirror/hello", withCreated(withRuntime(img("mirror/hello", "app/org.example.Hello/x86_64/stable", "amd64"), platform, false), t1)),
			repo("mirror/platform", img("mirror/platform", "runtime/org.example.Platform/x86_64/24.08", "amd64")),
			repo("other/alpha", alpha),
		)))

		keys := func(pkgs []*store.Package) (out []string) {
			for _, p := range pkgs {
				out = append(out, p.Kind+"/"+p.FlatpakID)
			}
			return out
		}
		all, total, err := s.ListPackages(ctx, store.PackageFilter{}, store.Page{})
		must(t, err)
		// Ordered by lower(display name): "alpha app", "hello beta" (the
		// newest Hello variant's name), "name org.example.codecs", "name
		// org.example.hello", "name org.example.platform" and, for the
		// nameless Zed, its flatpak ID.
		want := []string{"app/org.other.Alpha", "app/org.example.Hello", "runtime/org.example.Codecs", "runtime/org.example.Hello",
			"runtime/org.example.Platform", "app/org.example.Zed"}
		if total != 6 || !slices.Equal(keys(all), want) {
			t.Fatalf("all: %v (total %d), want %v", keys(all), total, want)
		}
		hello := all[1]
		if hello.Name != "Hello Beta" || hello.Version != "2.0" || hello.Summary != "Greets, soon" || hello.ImageCount != 4 ||
			!slices.Equal(hello.Architectures, []string{"amd64", "arm64"}) || !slices.Equal(hello.Branches, []string{"beta", "stable"}) ||
			len(hello.Registries) != 2 || hello.Registries[0].Name != "r1" || hello.Registries[0].ID != r1.ID || hello.Registries[1].Name != "r2" ||
			hello.HasExtraData || hello.Updated == nil || !hello.Updated.Equal(t3) {
			t.Errorf("hello: %+v", hello)
		}
		if hello.IconImageID == "" {
			t.Error("hello icon missing")
		} else if im, err := s.GetImage(ctx, hello.IconImageID); err != nil || im.Branch != "beta" {
			t.Errorf("hello icon image: %+v %v", im, err)
		}
		if codecs := all[2]; !codecs.HasExtraData || codecs.ImageCount != 1 || codecs.Name != "Name org.example.Codecs" {
			t.Errorf("codecs: %+v", codecs)
		}
		if rt := all[3]; rt.Kind != store.KindRuntime || rt.FlatpakID != "org.example.Hello" || rt.ImageCount != 1 || !slices.Equal(rt.Branches, []string{"24.08"}) {
			t.Errorf("hello runtime: %+v", rt)
		}
		if platform := all[4]; platform.ImageCount != 3 || len(platform.Registries) != 2 || platform.IconImageID != "" || platform.Updated != nil {
			t.Errorf("platform: %+v", platform)
		}
		if z := all[5]; z.Name != "" || z.Summary != "Summary of org.example.Zed" {
			t.Errorf("zed: %+v", z)
		}

		// Paging.
		page, total, err := s.ListPackages(ctx, store.PackageFilter{}, store.Page{Size: 2, Offset: 2})
		must(t, err)
		if total != 6 || !slices.Equal(keys(page), want[2:4]) {
			t.Errorf("page: %v (total %d)", keys(page), total)
		}
		if page, _, _ := s.ListPackages(ctx, store.PackageFilter{}, store.Page{Size: 2, Offset: 10}); len(page) != 0 || page == nil {
			t.Errorf("past the end: %v", page)
		}

		// Filters select packages; summaries still cover all their images.
		cases := []struct {
			name string
			f    store.PackageFilter
			want []string
		}{
			{"registry", store.PackageFilter{RegistryID: r2.ID}, []string{"app/org.other.Alpha", "app/org.example.Hello", "runtime/org.example.Platform"}},
			{"kind app", store.PackageFilter{Kind: store.KindApp}, []string{"app/org.other.Alpha", "app/org.example.Hello", "app/org.example.Zed"}},
			{"kind runtime", store.PackageFilter{Kind: store.KindRuntime}, []string{"runtime/org.example.Codecs", "runtime/org.example.Hello", "runtime/org.example.Platform"}},
			{"arch", store.PackageFilter{Architecture: "arm64"}, []string{"app/org.example.Hello", "runtime/org.example.Platform"}},
			{"query id", store.PackageFilter{Query: "ORG.EXAMPLE.HELLO"}, []string{"app/org.example.Hello", "runtime/org.example.Hello"}},
			{"query name", store.PackageFilter{Query: "hello beta"}, []string{"app/org.example.Hello"}},
			{"query summary", store.PackageFilter{Query: "summary of org.example.zed"}, []string{"app/org.example.Zed"}},
			{"query escaped", store.PackageFilter{Query: "%"}, nil},
			{"combined", store.PackageFilter{RegistryID: r1.ID, Kind: store.KindApp, Architecture: "amd64", Query: "hello"}, []string{"app/org.example.Hello"}},
			{"combined mismatch", store.PackageFilter{RegistryID: r2.ID, Kind: store.KindRuntime, Query: "hello"}, nil},
		}
		for _, c := range cases {
			got, total, err := s.ListPackages(ctx, c.f, store.Page{})
			must(t, err)
			if total != len(c.want) || !slices.Equal(keys(got), c.want) {
				t.Errorf("%s: %v (total %d), want %v", c.name, keys(got), total, c.want)
			}
		}
		fromR2, _, _ := s.ListPackages(ctx, store.PackageFilter{RegistryID: r2.ID}, store.Page{})
		if fromR2[1].ImageCount != 4 || len(fromR2[1].Registries) != 2 {
			t.Errorf("filtered package summary does not cover all images: %+v", fromR2[1])
		}

		// GetPackage: the same summary plus the variants in order.
		pkg, variants, err := s.GetPackage(ctx, store.KindApp, "org.example.Hello")
		must(t, err)
		if pkg.Name != hello.Name || pkg.ImageCount != 4 || pkg.IconImageID != hello.IconImageID || len(variants) != 4 {
			t.Errorf("get: %+v, %d variants", pkg, len(variants))
		}
		var order []string
		for _, v := range variants {
			order = append(order, v.Branch+"/"+v.Architecture+"/"+v.RegistryName+"/"+v.Repository)
			if v.Labels != nil {
				t.Error("variants loaded with labels")
			}
		}
		if want := []string{"beta/amd64/r1/apps/hello", "stable/amd64/r1/apps/hello", "stable/amd64/r2/mirror/hello", "stable/arm64/r1/apps/hello"}; !slices.Equal(order, want) {
			t.Errorf("variant order %v, want %v", order, want)
		}
		if variants[0].Runtime != platform || variants[3].Runtime != "org.example.Platform/aarch64/24.08" {
			t.Errorf("variant runtimes: %s, %s", variants[0].Runtime, variants[3].Runtime)
		}
		if _, v, err := s.GetPackage(ctx, store.KindRuntime, "org.example.Hello"); err != nil || len(v) != 1 {
			t.Errorf("runtime package: %d variants, %v", len(v), err)
		}
		if _, _, err := s.GetPackage(ctx, store.KindApp, "org.example.Nope"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("missing package: %v", err)
		}

		// Registries providing runtime refs, excluding one.
		refs := []string{"runtime/" + platform, "runtime/org.example.Platform/aarch64/24.08", "runtime/org.example.Nope/x86_64/1"}
		prov, err := s.RefProviders(ctx, refs, "")
		must(t, err)
		if len(prov) != 2 || len(prov[refs[0]]) != 2 || prov[refs[0]][0].Name != "r1" || prov[refs[0]][1].ID != r2.ID ||
			len(prov[refs[1]]) != 1 || prov[refs[1]][0].Name != "r1" {
			t.Errorf("providers: %+v", prov)
		}
		prov, err = s.RefProviders(ctx, refs, r1.ID)
		must(t, err)
		if len(prov) != 1 || len(prov[refs[0]]) != 1 || prov[refs[0]][0].Name != "r2" {
			t.Errorf("providers excluding r1: %+v", prov)
		}
		if prov, err := s.RefProviders(ctx, nil, ""); err != nil || len(prov) != 0 {
			t.Errorf("no refs: %v %v", prov, err)
		}
	})
}

func TestNewer(t *testing.T) {
	at := func(y int) *time.Time { v := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC); return &v }
	indexed := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	im := func(created *time.Time, id string) *store.Image {
		return &store.Image{ID: id, Created: created, IndexedAt: indexed}
	}
	for _, c := range []struct {
		name string
		a, b *store.Image
		want bool
	}{
		// Unix nanoseconds overflow after 2262: compare as times.
		{"2300 vs 2026", im(at(2300), "a"), im(at(2026), "b"), true},
		{"2026 vs 2300", im(at(2026), "a"), im(at(2300), "b"), false},
		{"2300 vs unknown", im(at(2300), "a"), im(nil, "b"), true},
		{"unknown vs 2300", im(nil, "a"), im(at(2300), "b"), false},
		{"1900 vs unknown", im(at(1900), "a"), im(nil, "b"), true},
		{"same time, later id", im(at(2026), "b"), im(at(2026), "a"), true},
		{"both unknown, later id", im(nil, "b"), im(nil, "a"), true},
		{"same", im(at(2026), "a"), im(at(2026), "a"), false},
	} {
		if got := store.Newer(c.a, c.b); got != c.want {
			t.Errorf("%s: Newer = %v, want %v", c.name, got, c.want)
		}
	}
	later := im(at(2026), "a")
	later.IndexedAt = indexed.Add(time.Second)
	if !store.Newer(later, im(at(2026), "b")) {
		t.Error("indexing time does not break the tie")
	}
}

// TestPackagesFarFutureCreated checks that SQL (ordering by the newest
// variant's name) and Go (the summary) agree on the newest variant when an
// upstream image claims a creation date after 2262.
func TestPackagesFarFutureCreated(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		s := tg.Open(t)
		r := newRegistry(t, s, "r")
		future, now := time.Date(2300, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		zeta := withCreated(img("x", "app/org.example.X/x86_64/stable", "amd64"), future)
		zeta.Name = "Zeta"
		alpha := withCreated(img("x", "app/org.example.X/aarch64/stable", "arm64"), now)
		alpha.Name = "Alpha"
		middle := withCreated(img("m", "app/org.example.M/x86_64/stable", "amd64"), now)
		middle.Name = "Middle"
		must(t, s.ReplaceRegistryImages(ctx, r.ID, "", ok(repo("x", zeta, alpha), repo("m", middle))))
		pkgs, _, err := s.ListPackages(ctx, store.PackageFilter{}, store.Page{})
		must(t, err)
		if len(pkgs) != 2 || pkgs[0].FlatpakID != "org.example.M" || pkgs[1].FlatpakID != "org.example.X" {
			t.Fatalf("order: %+v", pkgs)
		}
		if x := pkgs[1]; x.Name != "Zeta" || x.Updated == nil || !x.Updated.Equal(future) {
			t.Errorf("summary of X disagrees with the SQL order: name %q, updated %v", x.Name, x.Updated)
		}
	})
}
