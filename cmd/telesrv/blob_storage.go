package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	filesapp "telesrv/internal/app/files"
	"telesrv/internal/config"
	"telesrv/internal/domain"
)

type fileBlobBackendCounter interface {
	FileBlobBackendCounts(ctx context.Context) (map[domain.MediaBackend]int64, error)
}

// requireConfiguredBlobBackend rejects mixed or stale permanent-backend rows.
// Switching backends is an explicit migration operation, never a runtime read
// compatibility mode.
func requireConfiguredBlobBackend(ctx context.Context, counter fileBlobBackendCounter, configured string) error {
	counts, err := counter.FileBlobBackendCounts(ctx)
	if err != nil {
		return err
	}
	var mismatches []string
	for backend, count := range counts {
		if count > 0 && string(backend) != configured {
			mismatches = append(mismatches, fmt.Sprintf("%s=%d", backend, count))
		}
	}
	if len(mismatches) == 0 {
		return nil
	}
	sort.Strings(mismatches)
	return fmt.Errorf(
		"file_blobs contains permanent backend rows incompatible with TELESRV_BLOB_BACKEND=%s (%s); run the explicit blob migration before changing the configured backend",
		configured, strings.Join(mismatches, ", "),
	)
}

type blobStorageRuntime struct {
	permanent  filesapp.BlobBackend
	uploadPart filesapp.UploadPartBackend
}

type uniqueBlobBytesStore interface {
	UniqueFileBlobBytes(ctx context.Context, backend domain.MediaBackend) (int64, error)
}

type blobCapacityRuntime struct {
	permanentWrite filesapp.SpaceGuard
	stagingWrite   filesapp.SpaceGuard
	workers        []filesapp.SpaceGuard
}

func newBlobCapacityRuntime(ctx context.Context, cfg config.Config, store uniqueBlobBytesStore) (blobCapacityRuntime, error) {
	noop := filesapp.NoopSpaceGuard{}
	result := blobCapacityRuntime{permanentWrite: noop, stagingWrite: noop}
	if !cfg.StorageLowSpaceGuardEnable {
		return result, nil
	}

	localRoot := cfg.BlobDir
	if cfg.BlobBackendKind == string(domain.MediaBackendS3) {
		localRoot = cfg.BlobStagingDir
	}
	if cfg.StorageMinFreeBytes > 0 {
		local := filesapp.NewLocalDiskSpaceGuard(localRoot, cfg.StorageMinFreeBytes)
		if err := local.Refresh(ctx); err != nil {
			return blobCapacityRuntime{}, fmt.Errorf("initialize local storage capacity snapshot: %w", err)
		}
		result.stagingWrite = local
		result.permanentWrite = local
		result.workers = append(result.workers, local)
	}

	if cfg.BlobBackendKind == string(domain.MediaBackendS3) && cfg.StorageMaxTotalBytes > 0 {
		s3 := filesapp.NewS3BudgetSpaceGuard(store, cfg.StorageMaxTotalBytes)
		if err := s3.Refresh(ctx); err != nil {
			return blobCapacityRuntime{}, fmt.Errorf("initialize s3 storage capacity snapshot: %w", err)
		}
		result.workers = append(result.workers, s3)
		// S3FS first spools the stream locally and then publishes it to S3, so
		// both capacity targets must accept every permanent write.
		result.permanentWrite = filesapp.MultiSpaceGuard{result.stagingWrite, s3}
	}
	return result, nil
}

func newBlobStorageRuntime(ctx context.Context, cfg config.Config) (blobStorageRuntime, error) {
	switch cfg.BlobBackendKind {
	case string(domain.MediaBackendLocalFS):
		local, err := filesapp.NewLocalFS(cfg.BlobDir)
		if err != nil {
			return blobStorageRuntime{}, fmt.Errorf("init localfs blob backend: %w", err)
		}
		return blobStorageRuntime{permanent: local, uploadPart: local}, nil

	case string(domain.MediaBackendS3):
		staging, err := filesapp.NewLocalFS(cfg.BlobStagingDir)
		if err != nil {
			return blobStorageRuntime{}, fmt.Errorf("init local upload staging backend: %w", err)
		}
		s3, err := filesapp.NewS3FS(ctx, filesapp.S3Config{
			Endpoint:        cfg.S3Endpoint,
			Region:          cfg.S3Region,
			Bucket:          cfg.S3Bucket,
			AccessKeyID:     cfg.S3AccessKeyID,
			SecretAccessKey: cfg.S3SecretAccessKey,
			UseSSL:          cfg.S3UseSSL,
			PathStyle:       cfg.S3PathStyle,
			CreateBucket:    cfg.S3CreateBucket,
			StagingDir:      filepath.Join(cfg.BlobStagingDir, "_s3_spool"),
		})
		if err != nil {
			return blobStorageRuntime{}, fmt.Errorf("init s3 blob backend: %w", err)
		}
		return blobStorageRuntime{permanent: s3, uploadPart: staging}, nil

	default:
		return blobStorageRuntime{}, fmt.Errorf("unsupported blob backend %q", cfg.BlobBackendKind)
	}
}
