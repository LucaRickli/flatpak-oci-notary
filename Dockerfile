# syntax=docker/dockerfile:1

ARG DENO_VERSION=2.9.7
ARG GO_VERSION=1.27.1

# --- UI -----------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM denoland/deno:${DENO_VERSION} AS ui
WORKDIR /src/frontend
COPY frontend/package.json frontend/deno.lock frontend/.npmrc ./
RUN deno install --frozen
COPY frontend/ ./
RUN deno task build

# --- Server -------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS server
WORKDIR /src
RUN apk add --no-cache ca-certificates && mkdir -p /out/data
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=ui /src/frontend/build ./frontend/build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X github.com/lucarickli/flatpak-oci-notary/internal/version.Version=${VERSION} -X github.com/lucarickli/flatpak-oci-notary/internal/version.Commit=${COMMIT}" \
      -o /out/notary ./cmd/notary

# --- Runtime ------------------------------------------------------------------
FROM scratch
COPY --from=server /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=server /out/notary /notary
COPY --from=server --chown=65532:65532 /out/data /data
USER 65532:65532
ENV NOTARY_DATA_DIR=/data \
    NOTARY_LISTEN=:8080
VOLUME /data
EXPOSE 8080
# The default command runs the server; override it to run the standalone
# syncer against the same database, e.g. `docker run ... sync --due` or a
# Kubernetes CronJob with args: ["sync", "--due"] (see the README).
ENTRYPOINT ["/notary"]
CMD ["serve"]
