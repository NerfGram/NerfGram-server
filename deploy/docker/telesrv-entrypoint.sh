#!/bin/sh
set -e

mkdir -p /app/data/blobs /app/data/sticker-seed
chown -R nonroot:nonroot /app/data

exec su-exec nonroot /app/gramsrv "$@"
