package redisstore

import "testing"

func TestDecodeRateLimitIncrementResultAcceptsPTTLBoundary(t *testing.T) {
	count, ttlMillis, err := decodeRateLimitIncrementResult([]interface{}{int64(7), int64(0)})
	if err != nil {
		t.Fatalf("decode zero PTTL: %v", err)
	}
	if count != 7 || ttlMillis != 0 {
		t.Fatalf("decoded count=%d ttl=%d, want 7/0", count, ttlMillis)
	}
}

func TestDecodeRateLimitIncrementResultRejectsInvalidShape(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{name: "wrong type", value: "7,1"},
		{name: "wrong length", value: []interface{}{int64(7)}},
		{name: "zero count", value: []interface{}{int64(0), int64(1)}},
		{name: "negative ttl", value: []interface{}{int64(7), int64(-1)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := decodeRateLimitIncrementResult(test.value); err == nil {
				t.Fatal("expected decode error")
			}
		})
	}
}
