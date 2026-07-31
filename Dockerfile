# syntax=docker/dockerfile:1.4
#
# Multi-stage Dockerfile optimized for layer caching and small runtime image.
# Requires BuildKit (DOCKER_BUILDKIT=1). Cache mounts persist apk/go caches across builds.
#
ARG GO_VERSION=1.26
FROM golang:${GO_VERSION}-alpine AS builder
LABEL stage=builder
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

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /app/gramsrv ./cmd/telesrv

FROM alpine:3.21
LABEL stage=runtime
RUN --mount=type=cache,target=/var/cache/apk \
    apk add ca-certificates ffmpeg tzdata \
    && adduser -D -u 65532 -g '' nonroot
WORKDIR /app

COPY --from=builder /app/gramsrv /app/gramsrv

ENV TELESRV_BLOB_DIR=/app/data/blobs

VOLUME ["/app/data"]
EXPOSE 2398/tcp 2399/tcp

USER nonroot
ENTRYPOINT ["/app/gramsrv"]
