package indexer

import (
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func labelledImage(t *testing.T, arch string, labels map[string]string) v1.Image {
	t.Helper()
	img, err := random.Image(16, 1)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	cfg = cfg.DeepCopy()
	cfg.OS, cfg.Architecture, cfg.Config.Labels = "linux", arch, labels
	img, err = mutate.ConfigFile(img, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// TestToFlatpakImageValidation checks that images with refs or values the
// store cannot keep are treated as "not a flatpak" (and so never block a
// sync), while free text is cleaned.
func TestToFlatpakImageValidation(t *testing.T) {
	const digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	for _, c := range []struct {
		name, ref, arch string
		want            bool
	}{
		{"app", "app/org.example.App/x86_64/stable", "amd64", true},
		{"runtime", "runtime/org.example.Platform/x86_64/24.08", "amd64", true},
		{"unknown kind", "foo/org.example.App/x86_64/stable", "amd64", false},
		{"long kind", "applicationextension/org.example.App/x86_64/stable", "amd64", false},
		{"empty branch", "app/org.example.App/x86_64/", "amd64", false},
		{"too few parts", "app/org.example.App/x86_64", "amd64", false},
		{"too many parts", "app/org.example.App/x86_64/stable/x", "amd64", false},
		{"NUL in ref", "app/org.example.A\x00pp/x86_64/stable", "amd64", false},
		{"NUL in arch", "app/org.example.App/x86_64/stable", "amd\x0064", false},
	} {
		img := labelledImage(t, c.arch, map[string]string{LabelRef: c.ref, "version": "1.0\x00beta"})
		fi, err := toFlatpakImage("apps/x", digest, types.OCIManifestSchema1, img, nil)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if (fi != nil) != c.want {
			t.Errorf("%s: flatpak = %v, want %v", c.name, fi != nil, c.want)
		}
		if fi != nil && fi.Version != "1.0beta" {
			t.Errorf("%s: version %q not cleaned", c.name, fi.Version)
		}
	}
}
