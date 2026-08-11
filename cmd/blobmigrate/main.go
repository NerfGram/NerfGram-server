// Command blobmigrate performs an explicit offline permanent-blob backend
// migration. It never enables runtime fallback and never deletes the source
// objects; source cleanup is a separate, recoverable retention decision.
package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	filesapp "telesrv/internal/app/files"
	"telesrv/internal/config"
	"telesrv/internal/domain"
	"telesrv/internal/store/postgres"
)

const migrationReadChunk = int64(4 << 20)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "blob migration:", err)
		os.Exit(1)
	}
}

func run() error {
	fromFlag := flag.String("from", "", "source permanent backend: localfs or s3")
	toFlag := flag.String("to", "", "destination permanent backend: localfs or s3")
	batchFlag := flag.Int("batch", 250, "distinct object keys per PostgreSQL keyset page (1..10000)")
	dryRunFlag := flag.Bool("dry-run", false, "validate locks/config/metadata and report without copying or relabeling")
	flag.Parse()

	from := domain.MediaBackend(strings.ToLower(strings.TrimSpace(*fromFlag)))
	to := domain.MediaBackend(strings.ToLower(strings.TrimSpace(*toFlag)))
	if !validPermanentBackend(from) || !validPermanentBackend(to) || from == to {
		return fmt.Errorf("-from and -to must be different values from {localfs,s3}")
	}
	if *batchFlag <= 0 || *batchFlag > 10000 {
		return fmt.Errorf("-batch must be 1..10000")
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg.BlobBackendKind != string(from) {
		return fmt.Errorf(
			"TELESRV_BLOB_BACKEND=%s must name the current source backend %s",
			cfg.BlobBackendKind, from,
		)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	lock, err := postgres.AcquireBlobMigrationLock(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer lock.Close()

	pool, err := postgres.Open(ctx, cfg.PostgresDSN, postgres.WithMaxConns(4))
	if err != nil {
		return err
	}
	defer pool.Close()
	media := postgres.NewMediaStore(pool)
	source, err := newMigrationBackend(ctx, cfg, from, false)
	if err != nil {
		return fmt.Errorf("initialize source %s: %w", from, err)
	}
	target, err := newMigrationBackend(ctx, cfg, to, true)
	if err != nil {
		return fmt.Errorf("initialize destination %s: %w", to, err)
	}

	var (
		after         string
		objects       int64
		locationRows  int64
		migratedBytes int64
	)
	for {
		page, err := media.ListBlobMigrationObjects(ctx, from, after, *batchFlag)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}
		for _, object := range page {
			if err := ctx.Err(); err != nil {
				return err
			}
			objects++
			locationRows += object.LocationRows
			migratedBytes += object.Size
			if !*dryRunFlag {
				if err := migrateObject(ctx, source, target, object); err != nil {
					return fmt.Errorf("copy object %q: %w", object.ObjectKey, err)
				}
				if err := media.MoveFileBlobBackendForObject(
					ctx, from, to, object.ObjectKey, object.LocationRows,
				); err != nil {
					return err
				}
			}
			after = object.ObjectKey
		}
		fmt.Printf("checked objects=%d locations=%d bytes=%d last=%s\n", objects, locationRows, migratedBytes, after)
	}
	mode := "migrated"
	if *dryRunFlag {
		mode = "dry-run checked"
	}
	fmt.Printf("%s %s -> %s: objects=%d locations=%d bytes=%d\n", mode, from, to, objects, locationRows, migratedBytes)
	if !*dryRunFlag {
		fmt.Printf("source objects were retained; set TELESRV_BLOB_BACKEND=%s only after this command exits successfully\n", to)
	}
	return nil
}

func validPermanentBackend(backend domain.MediaBackend) bool {
	return backend == domain.MediaBackendLocalFS || backend == domain.MediaBackendS3
}

func newMigrationBackend(
	ctx context.Context,
	cfg config.Config,
	kind domain.MediaBackend,
	destination bool,
) (filesapp.BlobBackend, error) {
	switch kind {
	case domain.MediaBackendLocalFS:
		return filesapp.NewLocalFS(cfg.BlobDir)
	case domain.MediaBackendS3:
		return filesapp.NewS3FS(ctx, filesapp.S3Config{
			Endpoint:        cfg.S3Endpoint,
			Region:          cfg.S3Region,
			Bucket:          cfg.S3Bucket,
			AccessKeyID:     cfg.S3AccessKeyID,
			SecretAccessKey: cfg.S3SecretAccessKey,
			UseSSL:          cfg.S3UseSSL,
			PathStyle:       cfg.S3PathStyle,
			CreateBucket:    destination && cfg.S3CreateBucket,
			StagingDir:      filepath.Join(cfg.BlobStagingDir, "_migration_spool"),
		})
	default:
		return nil, fmt.Errorf("unsupported backend %q", kind)
	}
}

func migrateObject(
	ctx context.Context,
	source filesapp.BlobBackend,
	target filesapp.BlobBackend,
	object postgres.BlobMigrationObject,
) error {
	sourceReader := newBlobRangeReader(ctx, source, object.ObjectKey, object.Size)
	key, size, digest, err := target.PutReader(ctx, sourceReader)
	if err != nil {
		return err
	}
	if key != object.ObjectKey || size != object.Size || !equalBytes(digest, object.SHA256) {
		return fmt.Errorf(
			"destination write verification failed: key=%q size=%d digest=%x",
			key, size, digest,
		)
	}
	verifyReader := newBlobRangeReader(ctx, target, object.ObjectKey, object.Size)
	h := sha256.New()
	verifiedSize, err := io.Copy(h, verifyReader)
	if err != nil {
		return fmt.Errorf("read destination for verification: %w", err)
	}
	if verifiedSize != object.Size || !equalBytes(h.Sum(nil), object.SHA256) {
		return fmt.Errorf("destination read-back verification failed: size=%d digest=%x", verifiedSize, h.Sum(nil))
	}
	return nil
}

type blobRangeReader struct {
	ctx       context.Context
	backend   filesapp.BlobBackend
	objectKey string
	total     int64
	offset    int64
	buffer    []byte
}

func newBlobRangeReader(ctx context.Context, backend filesapp.BlobBackend, objectKey string, total int64) *blobRangeReader {
	return &blobRangeReader{ctx: ctx, backend: backend, objectKey: objectKey, total: total}
}

func (r *blobRangeReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.buffer) == 0 {
		if r.offset >= r.total {
			return 0, io.EOF
		}
		limit := migrationReadChunk
		if remaining := r.total - r.offset; remaining < limit {
			limit = remaining
		}
		data, total, err := r.backend.GetRange(r.ctx, r.objectKey, r.offset, limit)
		if err != nil {
			return 0, err
		}
		if total != r.total || int64(len(data)) != limit {
			return 0, fmt.Errorf("source range at %d returned total=%d bytes=%d, want total=%d bytes=%d", r.offset, total, len(data), r.total, limit)
		}
		r.buffer = data
	}
	n := copy(p, r.buffer)
	r.buffer = r.buffer[n:]
	r.offset += int64(n)
	return n, nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var different byte
	for i := range a {
		different |= a[i] ^ b[i]
	}
	return different == 0
}
