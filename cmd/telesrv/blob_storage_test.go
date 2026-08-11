package main

import (
	"context"
	"strings"
	"testing"

	"telesrv/internal/config"
	"telesrv/internal/domain"
)

type fixedBlobBackendCounts map[domain.MediaBackend]int64

func (f fixedBlobBackendCounts) FileBlobBackendCounts(context.Context) (map[domain.MediaBackend]int64, error) {
	return f, nil
}

func TestRequireConfiguredBlobBackend(t *testing.T) {
	ctx := context.Background()
	if err := requireConfiguredBlobBackend(ctx, fixedBlobBackendCounts{
		domain.MediaBackendLocalFS: 3,
	}, string(domain.MediaBackendLocalFS)); err != nil {
		t.Fatalf("matching backend rejected: %v", err)
	}
	if err := requireConfiguredBlobBackend(ctx, fixedBlobBackendCounts{}, string(domain.MediaBackendS3)); err != nil {
		t.Fatalf("empty database rejected: %v", err)
	}
	err := requireConfiguredBlobBackend(ctx, fixedBlobBackendCounts{
		domain.MediaBackendLocalFS: 2,
		domain.MediaBackendS3:      4,
	}, string(domain.MediaBackendS3))
	if err == nil || !strings.Contains(err.Error(), "localfs=2") {
		t.Fatalf("mismatched backend error = %v", err)
	}
}

func TestNewBlobStorageRuntimeDefaultsToOneLocalFS(t *testing.T) {
	runtime, err := newBlobStorageRuntime(context.Background(), config.Config{
		BlobBackendKind: string(domain.MediaBackendLocalFS),
		BlobDir:         t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new localfs runtime: %v", err)
	}
	if runtime.permanent.Name() != string(domain.MediaBackendLocalFS) {
		t.Fatalf("permanent backend = %q", runtime.permanent.Name())
	}
	if runtime.uploadPart == nil {
		t.Fatal("localfs upload-part backend is nil")
	}
}
