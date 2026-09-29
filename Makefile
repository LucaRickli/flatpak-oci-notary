VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w -X github.com/lucarickli/flatpak-oci-notary/internal/version.Version=$(VERSION) \
           -X github.com/lucarickli/flatpak-oci-notary/internal/version.Commit=$(COMMIT)

.PHONY: all generate lint test test-pg ui build dev-api dev-ui dev-sync docker clean

all: build

## generate: regenerate Go and TypeScript code from proto/ (needs buf and `deno install` in frontend/)
generate:
	buf generate

lint:
	buf lint
	go vet -tags noui ./...
	cd frontend && deno task check

test:
	go test -race -tags noui ./...
	cd frontend && deno task test

## test-pg: run the tests against Postgres as well, in a throwaway container
# Readiness is checked over TCP: during initdb the image runs a socket-only
# server that a plain pg_isready reports as ready. Gives up after a minute.
PG_DSN := postgres://notary:notary@localhost:55432/notary?sslmode=disable
test-pg:
	docker run -d --rm --name notary-test-pg -e POSTGRES_USER=notary -e POSTGRES_PASSWORD=notary -e POSTGRES_DB=notary -p 55432:5432 postgres:17-alpine
	@for i in $$(seq 60); do docker exec notary-test-pg pg_isready -h 127.0.0.1 -U notary >/dev/null 2>&1 && break; sleep 1; done; \
		docker exec notary-test-pg pg_isready -h 127.0.0.1 -U notary >/dev/null 2>&1 || { echo "postgres did not become ready"; docker stop notary-test-pg >/dev/null; exit 1; }
	NOTARY_TEST_POSTGRES_DSN='$(PG_DSN)' go test -race -tags noui ./... ; status=$$?; docker stop notary-test-pg >/dev/null; exit $$status

ui:
	cd frontend && deno install && deno task build

## build: static binary with the embedded UI at bin/notary
build: ui
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/notary ./cmd/notary

## dev-api: API on :8080 without auth and without embedded UI (use with dev-ui)
dev-api:
	go run -tags noui ./cmd/notary serve --auth-disabled --log-format console --log-level debug

## dev-ui: Vite dev server proxying /api, /auth, /icons and /repo to :8080
dev-ui:
	cd frontend && deno task dev

## dev-sync: sync the registries of the dev-api database once from a separate process
# (run dev-api with NOTARY_SYNC_ENABLED=false to try the split mode)
dev-sync:
	go run -tags noui ./cmd/notary sync --log-format console --log-level debug

docker:
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t flatpak-oci-notary:$(VERSION) .

clean:
	rm -rf bin frontend/build
