package main

import (
	"context"
	"crypto/sha256"
	"io"
	"strings"
	"testing"

	filesapp "telesrv/internal/app/files"
	"telesrv/internal/store/postgres"
)

func TestBlobRangeReaderAndMigrationRoundTrip(t *testing.T) {
	ctx := context.Background()
	source, err := filesapp.NewLocalFS(t.TempDir())
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	target, err := filesapp.NewLocalFS(t.TempDir())
	if err != nil {
		t.Fatalf("target: %v", err)
	}
	content := strings.Repeat("migration-range-", 400000)
	key, size, digest, err := source.PutReader(ctx, strings.NewReader(content))
	if err != nil {
		t.Fatalf("source put: %v", err)
	}
	reader := newBlobRangeReader(ctx, source, key, size)
	readBack, err := io.ReadAll(reader)
	if err != nil || string(readBack) != content {
		t.Fatalf("range reader len=%d err=%v", len(readBack), err)
	}
	if err := migrateObject(ctx, source, target, postgres.BlobMigrationObject{
		ObjectKey: key, Size: size, SHA256: digest, LocationRows: 1,
	}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	targetBytes, err := target.Get(ctx, key)
	if err != nil || sha256.Sum256(targetBytes) != sha256.Sum256([]byte(content)) {
		t.Fatalf("target verification len=%d err=%v", len(targetBytes), err)
	}
}
