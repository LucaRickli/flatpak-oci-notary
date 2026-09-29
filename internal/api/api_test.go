package api

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/rs/zerolog"

	notaryv1 "github.com/lucarickli/flatpak-oci-notary/internal/gen/notary/v1"
	"github.com/lucarickli/flatpak-oci-notary/internal/store"
	"github.com/lucarickli/flatpak-oci-notary/internal/store/storetest"
)

// Malformed input is reported as an invalid argument or not found, never as
// an internal error, on every dialect (Postgres rejects NUL in parameters).
func TestMalformedInputIsNeverInternal(t *testing.T) {
	storetest.Run(t, func(t *testing.T, tg storetest.Target) {
		s := tg.Open(t)
		a := New(s, nil, nil, zerolog.Nop())
		ctx := context.Background()
		reg, err := s.CreateRegistry(ctx, &store.Registry{Name: "r", URL: "https://example.com", SyncIntervalMinutes: 60})
		if err != nil {
			t.Fatal(err)
		}
		repo, err := s.CreateRepository(ctx, &store.Repository{Slug: "apps", RegistryID: reg.ID, Sources: []store.Source{{RepositoryPattern: "*"}}})
		if err != nil {
			t.Fatal(err)
		}
		const nul = "a\x00b"
		images, registries, repositories := imageService{a}, registryService{a}, repositoryService{a}
		kind := notaryv1.RefKind_REF_KIND_APP

		for _, tc := range []struct {
			name string
			call func() error
			want connect.Code
		}{
			{"GetPackage flatpak_id", func() error {
				_, err := images.GetPackage(ctx, connect.NewRequest(&notaryv1.GetPackageRequest{Kind: kind, FlatpakId: nul}))
				return err
			}, connect.CodeNotFound},
			{"ListPackages query", func() error {
				_, err := images.ListPackages(ctx, connect.NewRequest(&notaryv1.ListPackagesRequest{Query: nul}))
				return err
			}, connect.CodeInvalidArgument},
			{"ListPackages architecture", func() error {
				_, err := images.ListPackages(ctx, connect.NewRequest(&notaryv1.ListPackagesRequest{Architecture: nul}))
				return err
			}, connect.CodeInvalidArgument},
			{"ListImages flatpak_id", func() error {
				_, err := images.ListImages(ctx, connect.NewRequest(&notaryv1.ListImagesRequest{FlatpakId: nul}))
				return err
			}, connect.CodeInvalidArgument},
			{"ListImages query", func() error {
				_, err := images.ListImages(ctx, connect.NewRequest(&notaryv1.ListImagesRequest{Query: nul}))
				return err
			}, connect.CodeInvalidArgument},
			{"ListImages architecture", func() error {
				_, err := images.ListImages(ctx, connect.NewRequest(&notaryv1.ListImagesRequest{Architecture: nul}))
				return err
			}, connect.CodeInvalidArgument},
			{"ListImageRepositories query", func() error {
				_, err := images.ListImageRepositories(ctx, connect.NewRequest(&notaryv1.ListImageRepositoriesRequest{Query: nul}))
				return err
			}, connect.CodeInvalidArgument},
			{"CreateRegistry name", func() error {
				_, err := registries.CreateRegistry(ctx, connect.NewRequest(&notaryv1.CreateRegistryRequest{Registry: &notaryv1.RegistryInput{
					Name: nul, Url: "https://example.org", Repositories: []string{"x"},
				}}))
				return err
			}, connect.CodeInvalidArgument},
			{"CreateRegistry repositories", func() error {
				_, err := registries.CreateRegistry(ctx, connect.NewRequest(&notaryv1.CreateRegistryRequest{Registry: &notaryv1.RegistryInput{
					Name: "n", Url: "https://example.org", Repositories: []string{nul},
				}}))
				return err
			}, connect.CodeInvalidArgument},
			{"UpdateRepository title", func() error {
				_, err := repositories.UpdateRepository(ctx, connect.NewRequest(&notaryv1.UpdateRepositoryRequest{Id: repo.ID, Repository: &notaryv1.RepositoryInput{
					Slug: "apps", RegistryId: reg.ID, Title: nul,
				}}))
				return err
			}, connect.CodeInvalidArgument},
			{"UpdateRepository source", func() error {
				_, err := repositories.UpdateRepository(ctx, connect.NewRequest(&notaryv1.UpdateRepositoryRequest{Id: repo.ID, Repository: &notaryv1.RepositoryInput{
					Slug: "apps", RegistryId: reg.ID, Sources: []*notaryv1.Source{{RepositoryPattern: nul}},
				}}))
				return err
			}, connect.CodeInvalidArgument},
		} {
			if got := connect.CodeOf(tc.call()); got != tc.want {
				t.Errorf("%s: code %v, want %v", tc.name, got, tc.want)
			}
		}

		// Well-formed filters still work.
		if _, err := images.ListPackages(ctx, connect.NewRequest(&notaryv1.ListPackagesRequest{Query: "  ab  ", Architecture: "amd64"})); err != nil {
			t.Errorf("ListPackages: %v", err)
		}
		if _, err := images.GetPackage(ctx, connect.NewRequest(&notaryv1.GetPackageRequest{Kind: kind, FlatpakId: "org.example.None"})); connect.CodeOf(err) != connect.CodeNotFound {
			t.Errorf("GetPackage of an unknown package: %v", err)
		}

		// Ids are accepted in any case and answered in canonical (lowercase) form.
		got, err := registries.GetRegistry(ctx, connect.NewRequest(&notaryv1.GetRegistryRequest{Id: strings.ToUpper(reg.ID)}))
		if err != nil || got.Msg.Registry.Id != reg.ID {
			t.Errorf("GetRegistry(uppercase id) = %v, %v", got, err)
		}
	})
}
