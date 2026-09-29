# Flatpak OCI Notary

A small server that publishes **Flatpak remotes for flatpak images stored in OCI
registries** (ghcr.io, Quay, Harbor, Zot, registry.fedoraproject.org, …).

Flatpak can install from OCI registries through `oci+https://` remotes, but it
needs an _index_ that lists the flatpak images: `GET <remote>/index/static?…`.
Plain registries don't provide one. The notary indexes upstream registries and
serves that index. Flatpak then pulls manifests and blobs **directly from the
upstream registry**, so the notary never proxies image data.

```txt
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
- **Packages**: the indexed flatpaks (apps and runtimes), each grouping its
  images across architectures, branches, tags and registries, with appstream
  name, summary, version, icon, sizes, the runtime it needs and its labels.
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
`NOTARY_*` environment variable.

### Database

By default data lives in a single SQLite database, `<data-dir>/notary.db`
(`NOTARY_DATA_DIR=/data` in the image). This needs no setup and suits a single
server, optionally with a separate `notary sync` process on the same host (see
[Deployment modes](#deployment-modes)). SQLite is opened in WAL mode, which
only coordinates processes through the local file system: never put the
database on a network file system (NFS, SMB, ...) and never share it across
hosts. Registry passwords are stored in plain text, so protect the volume.

For larger deployments, or to run several replicas, use Postgres:

```sh
-e NOTARY_DATABASE_DRIVER=postgres \
-e NOTARY_DATABASE_DSN='postgres://notary:secret@db:5432/notary?sslmode=require'
```

The schema is created and migrated at startup. The database was redesigned
before the first release: a `notary.db` created by an earlier development
build is rejected at startup, delete it (`rm data/notary.db*`) and let the
registries sync again.

Running several replicas against one Postgres database:

- Give all replicas the same `session.secret`. Sessions are stateless
  HMAC-signed cookies, so any replica can verify a login made through another
  one; with a random per-start secret, sessions break as soon as the load
  balancer switches replicas.
- Registry syncs are coordinated through the database: a sync is claimed with
  a lease (`sync.lease`, default 2 minutes, renewed while the sync runs), so
  only one process syncs a given registry at a time even though every replica
  (or a separate `notary sync`) runs the scheduler. Lease times use the
  database clock. A sync interrupted by a shutdown (e.g. a rolling deploy) is
  released at once and taken over by another replica on its next check. If a
  replica dies mid-sync, the registry shows the sync as interrupted and is
  picked up again once the lease expires, or right away when the same instance
  restarts: a process only releases the syncs held under its own identity
  (`sync.instance`, default `<hostname>:<listen port>` for the server and
  `<hostname>:sync-<pid>-<random>`, new on every start, for `notary sync`).
  The identity must be unique among the processes sharing a database: two
  live processes with one identity release each other's syncs. To benefit
  from the immediate release it must also be stable across restarts, e.g.
  the pod name. An interrupted sync is retried on the next check even for a
  registry without a sync interval, so a requested sync is never lost.
- Syncs write incrementally: indexed repositories are stored in batches (every
  few seconds or 25 repositories) while the sync runs, so the served indexes,
  the package listing and the progress shown in the UI update as it goes.
  Repositories that vanished upstream are only removed once the sync has seen
  the whole registry, and a repository that fails to index keeps its images.
- Served indexes are cached per replica and invalidated through a generation
  counter in the database, so a change made through one replica is served by
  all of them within about a second. While the database is unreachable,
  cached indexes are still served.

### Deployment modes

**Embedded (default).** `notary serve` runs everything: indexes, admin API/UI
and the sync scheduler. This is the setup above and what the `docker run`
example gives you. Several replicas may run this way against Postgres.

**Split.** The server runs with `sync.enabled: false` (`NOTARY_SYNC_ENABLED=false`)
and only serves; a separate `notary sync` process, on the same database, does
the syncing. The UI then shows "Request sync" instead of "Sync now": the
request (also made automatically when a registry is created or changed) is
recorded in the database and picked up by the next syncer run. Use this to
give the syncer its own resources, schedule or network access, or to keep the
serving replicas free of sync load.

```sh
notary sync                    # sync every registry once, exit 1 if any failed
notary sync --due              # only registries whose interval elapsed, with a pending request or an interrupted sync
notary sync --registry fedora  # by name or id, repeatable
notary sync --watch            # run the scheduler as a long-lived worker
```

Registries currently being synced by another process are skipped. The
database and sync settings are the server's (`notary sync --help`).

Postgres is recommended for split deployments. SQLite works too when the
server and the syncer run on the same host and share the database directory
(a local volume, never a network file system): the server keeps serving from
its cache while the syncer writes.

Kubernetes, with a CronJob:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: notary }
spec:
  replicas: 2
  selector: { matchLabels: { app: notary } }
  template:
    metadata: { labels: { app: notary } }
    spec:
      containers:
        - name: notary
          image: ghcr.io/lucarickli/flatpak-oci-notary:edge
          # ENTRYPOINT is /notary and the default command "serve".
          ports: [{ containerPort: 8080 }]
          envFrom: [{ secretRef: { name: notary } }] # NOTARY_DATABASE_DSN, OIDC, session secret
          env:
            - { name: NOTARY_DATABASE_DRIVER, value: postgres }
            - { name: NOTARY_SYNC_ENABLED, value: "false" }
            - { name: NOTARY_PUBLIC_URL, value: https://flatpak.example.com }
---
apiVersion: batch/v1
kind: CronJob
metadata: { name: notary-sync }
spec:
  schedule: "*/5 * * * *"
  concurrencyPolicy: Forbid
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          containers:
            - name: sync
              image: ghcr.io/lucarickli/flatpak-oci-notary:edge
              args: ["sync", "--due"]
              envFrom: [{ secretRef: { name: notary } }]
              env:
                - { name: NOTARY_DATABASE_DRIVER, value: postgres }
```

Requests made through the UI wait for the next scheduled run (here up to five
minutes). For quicker pick-up, or when syncs take longer than the schedule
allows, run a worker Deployment instead of the CronJob:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: notary-sync }
spec:
  replicas: 1
  selector: { matchLabels: { app: notary-sync } }
  template:
    metadata: { labels: { app: notary-sync } }
    spec:
      containers:
        - name: sync
          image: ghcr.io/lucarickli/flatpak-oci-notary:edge
          args: ["sync", "--watch"]
          envFrom: [{ secretRef: { name: notary } }]
          env:
            - { name: NOTARY_DATABASE_DRIVER, value: postgres }
            # One identity per pod, stable across container restarts: a
            # restarted worker releases the syncs it left running at once.
            # Never set a fixed value here: during a rolling update the old
            # and the new pod would release each other's live syncs.
            - name: NOTARY_SYNC_INSTANCE
              valueFrom: { fieldRef: { fieldPath: metadata.name } }
```

A worker checks for due registries every `sync.check-interval` (30 s). Several
workers may run at once (more replicas, or a rollout overlapping old and new
pods), each with its own `sync.instance`; the lease keeps them from syncing
the same registry. A sync the old pod was running when it stopped is
released on shutdown and picked up by the next check.

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
make dev-sync  # one-shot "notary sync" against the dev-api database (try dev-api with NOTARY_SYNC_ENABLED=false)
make test      # Go tests (incl. an end-to-end test against an in-memory OCI registry) and frontend unit tests
make test-pg   # the same against Postgres too (starts a container via docker)
make lint      # buf lint, go vet, svelte-check
make generate  # regenerate Go/TS code after editing proto/
make build     # bin/notary with embedded UI
make docker    # container image (static binary on scratch)
```

Layout:

- `cmd/notary`: entry point (cobra/viper, zerolog): `serve` and `sync`
- `internal/indexer`: registry discovery and flatpak image indexing
  (go-containerregistry)
- `internal/flatpakindex`: index format, filtering and HTTP handler
- `internal/api`: Connect services
- `internal/auth`: OIDC login (PKCE) and HMAC-signed session cookies
- `internal/store`: persistence with GORM on SQLite (modernc, no cgo) or
  Postgres (pgx), versioned migrations (gormigrate), sync leases and the
  index generation counter
- `proto/`: API definition. Generated code lives in `internal/gen` and
  `frontend/src/lib/api/gen`.
- `frontend/`: SvelteKit + shadcn-svelte admin UI (static SPA embedded into the
  binary)

The store and end-to-end tests run against SQLite and, when
`NOTARY_TEST_POSTGRES_DSN` points to a Postgres database (each test uses a
fresh schema in it), against Postgres as well.

CI (`.github/workflows`) lints the protos, checks that generated code is up to
date, runs the Go and frontend checks (with a Postgres service container) and
builds the multi-arch image. Pushes to
`main` publish `ghcr.io/<owner>/flatpak-oci-notary:edge`. Publishing a GitHub
release with a semver tag (`1.2.3`, a leading `v` is optional) publishes the
image as `1.2.3`, `1.2`, `1` and `latest`, and attaches static binaries to the
release. Pre-releases (`1.3.0-rc.1`) only get their exact version tag.
