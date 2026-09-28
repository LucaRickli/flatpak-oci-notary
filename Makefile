VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w -X github.com/lucarickli/flatpak-oci-notary/internal/version.Version=$(VERSION) \
           -X github.com/lucarickli/flatpak-oci-notary/internal/version.Commit=$(COMMIT)

.PHONY: all generate lint test ui build dev-api dev-ui docker clean

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

docker:
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t flatpak-oci-notary:$(VERSION) .

clean:
	rm -rf bin frontend/build
