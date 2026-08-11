package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config configures the single permanent S3/MinIO blob backend. StagingDir
// is local scratch space used only while hashing a stream before its immutable
// content-addressed object key is known.
type S3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
	PathStyle       bool
	CreateBucket    bool
	StagingDir      string
}

// S3FS stores immutable, content-addressed permanent blobs in one S3-compatible
// bucket. It deliberately does not implement UploadPartBackend: transient upload
// parts remain in a separately configured local staging backend.
type S3FS struct {
	client     *minio.Client
	bucket     string
	stagingDir string
}

// NewS3FS creates the S3 backend and verifies bucket reachability. A missing
// bucket is created only when CreateBucket is explicitly enabled.
func NewS3FS(ctx context.Context, cfg S3Config) (*S3FS, error) {
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	cfg.Region = strings.TrimSpace(cfg.Region)
	cfg.Bucket = strings.TrimSpace(cfg.Bucket)
	cfg.StagingDir = strings.TrimSpace(cfg.StagingDir)
	if cfg.Endpoint == "" || strings.Contains(cfg.Endpoint, "://") {
		return nil, fmt.Errorf("s3 endpoint must be host[:port] without a URL scheme")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("s3 bucket is required")
	}
	if strings.TrimSpace(cfg.AccessKeyID) == "" || strings.TrimSpace(cfg.SecretAccessKey) == "" {
		return nil, fmt.Errorf("s3 access key id and secret access key are required")
	}
	if cfg.StagingDir == "" {
		return nil, fmt.Errorf("s3 staging directory is required")
	}
	if err := os.MkdirAll(cfg.StagingDir, 0o700); err != nil {
		return nil, fmt.Errorf("create s3 staging directory: %w", err)
	}

	lookup := minio.BucketLookupAuto
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure:       cfg.UseSSL,
		Region:       cfg.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("create s3 client: %w", err)
	}
	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("check s3 bucket %q: %w", cfg.Bucket, err)
	}
	if !exists {
		if !cfg.CreateBucket {
			return nil, fmt.Errorf("s3 bucket %q does not exist and automatic creation is disabled", cfg.Bucket)
		}
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			return nil, fmt.Errorf("create s3 bucket %q: %w", cfg.Bucket, err)
		}
	}
	return &S3FS{client: client, bucket: cfg.Bucket, stagingDir: cfg.StagingDir}, nil
}

func (s *S3FS) Name() string { return "s3" }

func (s *S3FS) Put(ctx context.Context, data []byte) (string, error) {
	key, _, _, err := s.PutReader(ctx, bytes.NewReader(data))
	return key, err
}

// PutReader first hashes into bounded local scratch storage, then publishes the
// final SHA-256 key to S3. The database row is written by the caller only after
// this method returns success.
func (s *S3FS) PutReader(ctx context.Context, r io.Reader) (string, int64, []byte, error) {
	if r == nil {
		return "", 0, nil, fmt.Errorf("s3 blob reader is nil")
	}
	if err := ctx.Err(); err != nil {
		return "", 0, nil, err
	}
	tmp, err := os.CreateTemp(s.stagingDir, "blob-s3-*.tmp")
	if err != nil {
		return "", 0, nil, fmt.Errorf("create s3 blob staging file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	h := sha256.New()
	size, copyErr := copyWithContext(ctx, io.MultiWriter(tmp, h), r)
	closeErr := tmp.Close()
	if copyErr != nil {
		return "", 0, nil, fmt.Errorf("stage s3 blob: %w", copyErr)
	}
	if closeErr != nil {
		return "", 0, nil, fmt.Errorf("close s3 blob staging file: %w", closeErr)
	}
	sum := h.Sum(nil)
	key := hex.EncodeToString(sum)

	if info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{}); err == nil {
		if info.Size != size {
			return "", 0, nil, fmt.Errorf("existing s3 blob %q size %d does not match content size %d", key, info.Size, size)
		}
		return key, size, append([]byte(nil), sum...), nil
	} else if !isS3NotFound(err) {
		return "", 0, nil, fmt.Errorf("stat s3 blob %q: %w", key, err)
	}

	f, err := os.Open(filepath.Clean(tmpPath))
	if err != nil {
		return "", 0, nil, fmt.Errorf("reopen s3 blob staging file: %w", err)
	}
	defer f.Close()
	uploaded, err := s.client.PutObject(ctx, s.bucket, key, f, size, minio.PutObjectOptions{})
	if err != nil {
		return "", 0, nil, fmt.Errorf("put s3 blob %q: %w", key, err)
	}
	if uploaded.Size != size {
		return "", 0, nil, fmt.Errorf("put s3 blob %q reported size %d, want %d", key, uploaded.Size, size)
	}
	return key, size, append([]byte(nil), sum...), nil
}

func (s *S3FS) Get(ctx context.Context, objectKey string) ([]byte, error) {
	data, _, err := s.GetRange(ctx, objectKey, 0, 0)
	return data, err
}

// GetRange returns exactly [offset,min(offset+limit,total)). A storage error is
// never converted to an empty successful response because Telegram clients use
// an empty/short part as an EOF signal.
func (s *S3FS) GetRange(ctx context.Context, objectKey string, offset, limit int64) ([]byte, int64, error) {
	info, err := s.client.StatObject(ctx, s.bucket, objectKey, minio.StatObjectOptions{})
	if err != nil {
		return nil, 0, fmt.Errorf("stat s3 blob %q: %w", objectKey, err)
	}
	total := info.Size
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []byte{}, total, nil
	}
	length := total - offset
	if limit > 0 && limit < length {
		length = limit
	}
	end := offset + length - 1
	opts := minio.GetObjectOptions{}
	if err := opts.SetRange(offset, end); err != nil {
		return nil, 0, fmt.Errorf("set s3 range [%d,%d]: %w", offset, end, err)
	}
	obj, err := s.client.GetObject(ctx, s.bucket, objectKey, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("get s3 blob %q range: %w", objectKey, err)
	}
	defer obj.Close()
	data, err := io.ReadAll(io.LimitReader(obj, length+1))
	if err != nil {
		return nil, 0, fmt.Errorf("read s3 blob %q range: %w", objectKey, err)
	}
	if int64(len(data)) != length {
		return nil, 0, fmt.Errorf("read s3 blob %q range returned %d bytes, want %d", objectKey, len(data), length)
	}
	return data, total, nil
}

// Delete is used by explicit migration/retention workflows. Missing objects are
// already idempotent with S3 RemoveObject semantics; callers must first prove no
// live file_blobs row references the content-addressed key.
func (s *S3FS) Delete(ctx context.Context, objectKey string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, objectKey, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete s3 blob %q: %w", objectKey, err)
	}
	return nil
}

func isS3NotFound(err error) bool {
	response := minio.ToErrorResponse(err)
	return response.Code == "NoSuchKey" || response.Code == "NotFound" || response.StatusCode == 404
}
