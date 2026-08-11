package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

func TestReadStoreStorageStatsDeduplicatesPhysicalObjects(t *testing.T) {
	store, pool := verificationReadStore(t)
	ctx := context.Background()
	before, err := store.StorageStats(ctx)
	if err != nil {
		t.Fatalf("StorageStats before: %v", err)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	sum := sha256.Sum256([]byte("admin-storage:" + suffix))
	objectKey := hex.EncodeToString(sum[:])
	locations := []string{"admin-storage:a:" + suffix, "admin-storage:b:" + suffix}
	for _, location := range locations {
		if _, err := pool.Exec(ctx, `
INSERT INTO file_blobs (location_key, backend, object_key, size, sha256, mime_type)
VALUES ($1, 'localfs', $2, 123, $3, 'application/octet-stream')`, location, objectKey, sum[:]); err != nil {
			t.Fatalf("insert file blob %s: %v", location, err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM file_blobs WHERE location_key = ANY($1::text[])`, locations)
	})

	after, err := store.StorageStats(ctx)
	if err != nil {
		t.Fatalf("StorageStats after: %v", err)
	}
	if delta := after.PhysicalBytes - before.PhysicalBytes; delta != 123 {
		t.Fatalf("physical byte delta=%d, want shared object counted once", delta)
	}
	if delta := after.LogicalBytes - before.LogicalBytes; delta != 246 {
		t.Fatalf("logical byte delta=%d, want both location references", delta)
	}
	if after.ObjectCount-before.ObjectCount != 1 || after.ReferenceCount-before.ReferenceCount != 2 {
		t.Fatalf("object/reference deltas=%d/%d, want 1/2", after.ObjectCount-before.ObjectCount, after.ReferenceCount-before.ReferenceCount)
	}
}
