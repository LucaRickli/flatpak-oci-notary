package indexer

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/lucarickli/flatpak-oci-notary/internal/store"
)

// TestResult is the outcome of TestRegistry.
type TestResult struct {
	OK                 bool
	Error              string
	CatalogSupported   bool
	SampleRepositories []string
}

// TestRegistry checks connectivity and credentials without persisting anything.
func TestRegistry(ctx context.Context, reg *store.Registry) TestResult {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c, err := newClient(ctx, reg)
	if err != nil {
		return TestResult{Error: err.Error()}
	}

	var res TestResult
	sample, catalogErr := remote.CatalogPage(c.registry, "", 50, c.options...)
	if catalogErr == nil {
		res.CatalogSupported = true
		res.SampleRepositories = sample
		if res.SampleRepositories == nil {
			res.SampleRepositories = []string{}
		}
	}

	// Verify access to the explicitly configured repositories.
	for _, r := range reg.Repositories {
		if _, err := remote.List(c.repo(r), c.options...); err != nil {
			res.Error = fmt.Sprintf("list tags of %s: %v", r, err)
			return res
		}
	}
	switch {
	case len(reg.Repositories) > 0 || res.CatalogSupported:
		res.OK = true
	case reg.UseCatalog:
		res.Error = fmt.Sprintf("catalog not available: %v", catalogErr)
	default:
		res.Error = "catalog not available and no repositories configured; add repositories to verify access"
		if catalogErr != nil {
			res.Error += fmt.Sprintf(" (catalog: %v)", catalogErr)
		}
	}
	return res
}
