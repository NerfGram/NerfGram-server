#
# Multi-stage Dockerfile optimized for layer caching and small runtime image.
# - First stage builds the Go binary with module download caching.
# - Final stage uses a minimal distroless/static base with CA certs for TLS.
#
ARG GO_VERSION=1.26
FROM golang:${GO_VERSION}-alpine AS builder
LABEL stage=builder
WORKDIR /src

# Keep common packages in separate layer to leverage cache (apk + go mod download).
# Install system packages with cache mount to speed repeated builds (BuildKit)
RUN --mount=type=cache,target=/var/cache/apk apk add git ca-certificates tzdata

# Use build args for reproducible and cache-friendly builds.
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

# Copy module files first to leverage Docker layer caching for go mod download.
COPY go.mod go.sum ./
# Cache Go module download and build cache between builds (BuildKit)
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build go mod download

# Copy the rest of the source tree.
COPY . .

# Build a small, statically-linked binary.
# Use cache mounts for the Go build cache and module cache to speed incremental builds (requires BuildKit).
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /app/gramsrv ./cmd/telesrv

# Final runtime image: distroless static (non-root)
FROM gcr.io/distroless/static:nonroot
LABEL stage=runtime
WORKDIR /app

# Copy binary and CA certs from builder.
COPY --from=builder /app/gramsrv /app/gramsrv
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# Create a mount point for persistent server data (keys, blobs).
VOLUME ["/app/data"]
ENV TELESRV_BLOB_DIR=/app/data/blobs

# Expose commonly used ports (MTProto + admin)
EXPOSE 2398/tcp 2399/tcp

# Use non-root user provided by distroless (numeric uid/gid set by image).
USER nonroot

ENTRYPOINT ["/app/gramsrv"]

