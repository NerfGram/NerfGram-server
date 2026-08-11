package files

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestS3FSMinIOIntegration(t *testing.T) {
	endpoint := os.Getenv("TELESRV_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("TELESRV_TEST_S3_ENDPOINT is not set")
	}
	accessKey := os.Getenv("TELESRV_TEST_S3_ACCESS_KEY_ID")
	secretKey := os.Getenv("TELESRV_TEST_S3_SECRET_ACCESS_KEY")
	if accessKey == "" || secretKey == "" {
		t.Fatal("TELESRV_TEST_S3_ACCESS_KEY_ID and TELESRV_TEST_S3_SECRET_ACCESS_KEY are required")
	}
	var suffix [10]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("random bucket suffix: %v", err)
	}
	bucket := "telesrv-test-" + hex.EncodeToString(suffix[:])
	ctx := context.Background()
	backend, err := NewS3FS(ctx, S3Config{
		Endpoint:        endpoint,
		Region:          "us-east-1",
		Bucket:          bucket,
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
		UseSSL:          false,
		PathStyle:       true,
		CreateBucket:    true,
		StagingDir:      t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new s3 backend: %v", err)
	}
	t.Cleanup(func() {
		_ = backend.client.RemoveBucket(context.Background(), bucket)
	})

	content := "0123456789-" + strings.Repeat("minio-range-", 2048)
	key, size, sum, err := backend.PutReader(ctx, strings.NewReader(content))
	if err != nil {
		t.Fatalf("put reader: %v", err)
	}
	if size != int64(len(content)) || len(sum) != 32 || key != hex.EncodeToString(sum) {
		t.Fatalf("put metadata key=%q size=%d sha_len=%d", key, size, len(sum))
	}
	t.Cleanup(func() { _ = backend.Delete(context.Background(), key) })

	duplicate, err := backend.Put(ctx, []byte(content))
	if err != nil || duplicate != key {
		t.Fatalf("dedup key=%q err=%v, want %q", duplicate, err, key)
	}
	rangeBytes, total, err := backend.GetRange(ctx, key, 3, 17)
	if err != nil {
		t.Fatalf("get range: %v", err)
	}
	if total != size || string(rangeBytes) != content[3:20] {
		t.Fatalf("range total=%d bytes=%q", total, rangeBytes)
	}
	eof, total, err := backend.GetRange(ctx, key, size, 128<<10)
	if err != nil || total != size || len(eof) != 0 {
		t.Fatalf("EOF range total=%d len=%d err=%v", total, len(eof), err)
	}
	all, err := backend.Get(ctx, key)
	if err != nil || string(all) != content {
		t.Fatalf("get all len=%d err=%v", len(all), err)
	}
	if _, _, err := backend.GetRange(ctx, strings.Repeat("0", 64), 0, 1); err == nil {
		t.Fatal("missing S3 object returned empty success")
	}
}
