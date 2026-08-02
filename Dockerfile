# syntax=docker/dockerfile:1.4
#
# Multi-target Dockerfile (BuildKit required for cache mounts).
#
#   docker build --target telesrv -t telesrv-server:local .
#   docker build --target admin  -t telesrv-admin:local .
#
ARG GO_VERSION=1.26

# --- shared module cache / source tree -----------------------------------------
FROM golang:${GO_VERSION}-alpine AS deps
WORKDIR /src

RUN --mount=type=cache,target=/var/cache/apk \
    apk add git ca-certificates tzdata

ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

# --- compile binaries ----------------------------------------------------------
FROM deps AS build-telesrv
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /app/gramsrv ./cmd/telesrv

FROM deps AS build-admin
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /app/telesrv-admin ./cmd/telesrv-admin

# --- runtime images ------------------------------------------------------------
FROM gcr.io/distroless/static:nonroot AS admin
WORKDIR /app

COPY --from=build-admin /app/telesrv-admin /app/telesrv-admin
COPY --from=build-admin /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

VOLUME ["/app/data"]
EXPOSE 2600/tcp

USER nonroot
ENTRYPOINT ["/app/telesrv-admin"]

FROM alpine:3.21 AS telesrv
RUN --mount=type=cache,target=/var/cache/apk \
    apk add ca-certificates ffmpeg tzdata \
    && adduser -D -u 65532 -g '' nonroot
WORKDIR /app

COPY --from=build-telesrv /app/gramsrv /app/gramsrv

ENV TELESRV_BLOB_DIR=/app/data/blobs

VOLUME ["/app/data"]
EXPOSE 2398/tcp 2399/tcp

USER nonroot
ENTRYPOINT ["/app/gramsrv"]
