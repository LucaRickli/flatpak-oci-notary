package indexer

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/lucarickli/flatpak-oci-notary/internal/store"
	"github.com/lucarickli/flatpak-oci-notary/internal/version"
)

// ParseRegistryURL validates a registry base URL and returns the host (with
// port) and whether it uses plain http.
func ParseRegistryURL(raw string) (host string, plainHTTP bool, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false, fmt.Errorf("invalid registry URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", false, fmt.Errorf("registry URL must start with https:// or http://")
	}
	if u.Host == "" {
		return "", false, fmt.Errorf("registry URL has no host")
	}
	if u.Path != "" && u.Path != "/" {
		return "", false, fmt.Errorf("registry URL must not contain a path (OCI registries are served from the host root)")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", false, fmt.Errorf("registry URL must not contain credentials, query or fragment")
	}
	return u.Host, u.Scheme == "http", nil
}

// NormalizeRegistryURL returns the canonical "scheme://host" form. Docker Hub
// aliases are mapped to its API host, since flatpak pulls from the URL as-is.
func NormalizeRegistryURL(raw string) (string, error) {
	host, plain, err := ParseRegistryURL(raw)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(host) {
	case "docker.io", "index.docker.io", "hub.docker.com":
		host = "registry-1.docker.io"
	}
	if plain {
		return "http://" + host, nil
	}
	return "https://" + host, nil
}

// client bundles what is needed to talk to one registry.
type client struct {
	registry name.Registry
	options  []remote.Option
}

func newClient(ctx context.Context, reg *store.Registry) (*client, error) {
	host, plain, err := ParseRegistryURL(reg.URL)
	if err != nil {
		return nil, err
	}
	var nameOpts []name.Option
	if plain || reg.Insecure {
		nameOpts = append(nameOpts, name.Insecure)
	}
	r, err := name.NewRegistry(host, nameOpts...)
	if err != nil {
		return nil, err
	}

	var auth authn.Authenticator = authn.Anonymous
	if reg.AuthType == store.AuthBasic && (reg.Username != "" || reg.Password != "") {
		auth = &authn.Basic{Username: reg.Username, Password: reg.Password}
	}

	tr := remote.DefaultTransport.(*http.Transport).Clone()
	if reg.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicitly requested per registry
	}

	return &client{
		registry: r,
		options: []remote.Option{
			remote.WithContext(ctx),
			remote.WithAuth(auth),
			remote.WithTransport(tr),
			remote.WithUserAgent("flatpak-oci-notary/" + version.Version),
		},
	}, nil
}

func (c *client) repo(path string) name.Repository { return c.registry.Repo(path) }
