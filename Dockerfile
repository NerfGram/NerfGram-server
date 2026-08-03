# syntax=docker/dockerfile:1.4
#
#   docker compose up --build
#
# Official star-gift / NFT assets ship from data/official-gifts/ in this repo
# (no external CDN or third-party checkout).
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

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY deploy/ ./deploy/

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
    apk add ca-certificates ffmpeg tzdata su-exec \
    && adduser -D -u 65532 -g '' nonroot
WORKDIR /app

COPY --from=build-telesrv /app/gramsrv /app/gramsrv
COPY deploy/docker/telesrv-entrypoint.sh /app/entrypoint.sh
COPY downloads/FromGram.png /app/downloads/FromGram.png
# Last: large read-only catalog; code-only rebuilds keep this layer cached.
COPY data/official-gifts /app/official-gifts
RUN sed -i 's/\r$//' /app/entrypoint.sh && chmod +x /app/entrypoint.sh

ENV TELESRV_BLOB_DIR=/app/data/blobs
ENV TELESRV_OFFICIAL_GIFTS_DIR=/app/official-gifts
ENV TELESRV_STICKER_SEED_DIR=/app/data/sticker-seed
ENV TELESRV_SYSTEM_AVATAR_PATH=/app/downloads/FromGram.png

VOLUME ["/app/data"]
EXPOSE 2398/tcp 2399/tcp

ENTRYPOINT ["/app/entrypoint.sh"]
