# Flatpak OCI Notary

A small server that publishes **Flatpak remotes for flatpak images stored in OCI
registries** (ghcr.io, Quay, Harbor, Zot, registry.fedoraproject.org, …).

Flatpak can install from OCI registries through `oci+https://` remotes, but it
needs an _index_ that lists the flatpak images: `GET <remote>/index/static?…`.
Plain registries don't provide one. The notary indexes upstream registries and
serves that index. Flatpak then pulls manifests and blobs **directly from the
upstream registry**, so the notary never proxies image data.

```
flatpak ──GET /repo/<slug>/index/static──▶ notary ──indexes──▶ OCI registry
   └──────────── pulls manifests & blobs directly ────────────────▲
```

## Concepts

- **Registries**: upstream OCI registries, with anonymous or basic
  (username + password/token) auth. Repositories are discovered through
  `/v2/_catalog` where the registry supports it; ghcr.io and Docker Hub don't,
  so you can also list repositories explicitly. You can narrow discovery with
  repository and tag globs. Registries are synced periodically, and an image
  counts as a flatpak when its config carries the `org.flatpak.ref` label.
- **Packages**: indexed flatpak images (per architecture), with appstream name,
  summary, version, icon, sizes and labels.
- **Repositories**: what you publish. Each one has a slug (the URL), a title and
  an ordered list of **source rules** (`repository`, `ref` and `tag` globs,
  include or exclude). An image is served if at least one include rule matches
  it and no exclude rule does. For example, include `myorg/*` and exclude
  `app/org.example.Internal*`. If several images carry the same ref, the newest
  one is served.

**One registry per repository.** Flatpak's index format has a single `Registry`
base URL, and flatpak resolves every image name against it. A repository can
therefore only serve images from one upstream registry. To combine registries,
create one repository per registry and add several remotes. (A pull-through
proxy mode could lift this limit, but then the notary would carry the image
traffic and expose private upstream images through its own credentials.)

**Authentication for clients** stays with the registry. The index itself is
public. For private upstream images, flatpak authenticates to the registry
directly, using credentials from `~/.config/flatpak/oci-auth.json`,
`/etc/flatpak/oci-auth.json` or `$XDG_RUNTIME_DIR/containers/auth.json`.
The admin UI and API are protected by OIDC.

## Using a repository

```sh
flatpak remote-add --if-not-exists --no-gpg-verify myrepo oci+https://flatpak.example.com/repo/myrepo
# or
flatpak remote-add --if-not-exists myrepo https://flatpak.example.com/repo/myrepo.flatpakrepo
```

Flatpak only asks for images tagged `latest`. To follow another tag, append it
as a fragment: `oci+https://flatpak.example.com/repo/myrepo#beta`.

## Running

```sh
docker run -p 8080:8080 -v notary-data:/data \
  -e NOTARY_PUBLIC_URL=https://flatpak.example.com \
  -e NOTARY_OIDC_ISSUER=https://auth.example.com/realms/main \
  -e NOTARY_OIDC_CLIENT_ID=flatpak-oci-notary \
  -e NOTARY_OIDC_CLIENT_SECRET=… \
  -e NOTARY_OIDC_ALLOWED_GROUPS=flatpak-admins \
  -e NOTARY_SESSION_SECRET=$(openssl rand -hex 32) \
  ghcr.io/lucarickli/flatpak-oci-notary:edge
```

Register `<public-url>/auth/callback` as the redirect URI with your identity
provider. See [`notary.example.yaml`](notary.example.yaml) for all options.
Each one can also be set as a flag (`notary serve --help`) or through a
`NOTARY_*` environment variable. Data lives in a single SQLite database in
`data-dir`. Registry passwords are stored there in plain text, so protect the
volume.

HTTP endpoints:

| Path                            | Purpose                                                   |
| ------------------------------- | --------------------------------------------------------- |
| `/repo/<slug>/index/static`     | Flatpak index (also `/index/dynamic`), gzip + ETag        |
| `/repo/<slug>.flatpakrepo`      | `.flatpakrepo` file                                       |
| `/api/notary.v1.*`              | Connect admin API ([proto](proto/notary/v1/notary.proto)) |
| `/auth/login`, `/auth/callback` | OIDC login                                                |
| `/icons/<id>`                   | Package icons for the UI                                  |
| `/healthz`                      | Liveness                                                  |

## Development

Requirements: Go, Deno, buf.

```sh
cd frontend && deno install && cd ..
make dev-api   # API on :8080, auth disabled, no embedded UI
make dev-ui    # Vite on :5173, proxies to :8080
make test      # Go tests (incl. an end-to-end test against an in-memory OCI registry)
make lint      # buf lint, go vet, svelte-check
make generate  # regenerate Go/TS code after editing proto/
make build     # bin/notary with embedded UI
make docker    # container image (static binary on scratch)
```

Layout:

- `cmd/notary`: entry point (cobra/viper, zerolog)
- `internal/indexer`: registry discovery and flatpak image indexing
  (go-containerregistry)
- `internal/flatpakindex`: index format, filtering and HTTP handler
- `internal/api`: Connect services
- `internal/auth`: OIDC login (PKCE) and HMAC-signed session cookies
- `internal/store`: SQLite (modernc, no cgo)
- `proto/`: API definition. Generated code lives in `internal/gen` and
  `frontend/src/lib/api/gen`.
- `frontend/`: SvelteKit + shadcn-svelte admin UI (static SPA embedded into the
  binary)

CI (`.github/workflows`) lints the protos, checks that generated code is up to
date, runs the Go and frontend checks and builds the multi-arch image. Pushes to
`main` publish `ghcr.io/<owner>/flatpak-oci-notary:edge`. Publishing a GitHub
release with a semver tag (`1.2.3`, a leading `v` is optional) publishes the
image as `1.2.3`, `1.2`, `1` and `latest`, and attaches static binaries to the
release. Pre-releases (`1.3.0-rc.1`) only get their exact version tag.
