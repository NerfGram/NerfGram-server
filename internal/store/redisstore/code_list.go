package redisstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"telesrv/internal/store"
)

// ActivePhoneCode is a live phonecode:* record plus its remaining TTL.
// Used by the admin console; not part of store.CodeStore.
type ActivePhoneCode struct {
	Hash           string `json:"hash"`
	Phone          string `json:"phone"`
	Email          string `json:"email"`
	PendingEmail   string `json:"pending_email"`
	Code           string `json:"code"`
	Channel        string `json:"channel"`
	Purpose        string `json:"purpose"`
	UserID         int64  `json:"user_id,string"`
	IssuedUserID   int64  `json:"issued_user_id,string"`
	Attempts       int    `json:"attempts"`
	MaxAttempts    int    `json:"max_attempts"`
	SignUpVerified bool   `json:"sign_up_verified"`
	RequireSignUp  bool   `json:"require_sign_up"`
	VerifiedEmail  bool   `json:"verified_email"`
	TTLSeconds     int64  `json:"ttl_seconds"`
}

const listActiveCodeScanCount = 200
const listActiveCodeMaxKeys = 2000

// ListActive scans Redis for live phonecode:* records. Scope index keys
// (phonecodescope:*) are ignored. Undecodable values are skipped.
func (s *CodeStore) ListActive(ctx context.Context) ([]ActivePhoneCode, error) {
	if s == nil || s.c == nil {
		return nil, fmt.Errorf("redis code store is not configured")
	}
	var (
		cursor uint64
		out    = make([]ActivePhoneCode, 0)
	)
	for {
		keys, next, err := s.c.Scan(ctx, cursor, codeKeyPrefix+"*", listActiveCodeScanCount).Result()
		if err != nil {
			return nil, fmt.Errorf("redis scan phone codes: %w", err)
		}
		for _, key := range keys {
			if len(out) >= listActiveCodeMaxKeys {
				return out, nil
			}
			hash := strings.TrimPrefix(key, codeKeyPrefix)
			if hash == "" || hash == key {
				continue
			}
			pipe := s.c.Pipeline()
			getCmd := pipe.Get(ctx, key)
			ttlCmd := pipe.PTTL(ctx, key)
			if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
				return nil, fmt.Errorf("redis load phone code %s: %w", hash, err)
			}
			raw, err := getCmd.Bytes()
			if err != nil {
				if err == redis.Nil {
					continue
				}
				return nil, fmt.Errorf("redis get phone code %s: %w", hash, err)
			}
			var code store.PhoneCode
			if err := json.Unmarshal(raw, &code); err != nil {
				continue
			}
			ttlSeconds := int64(0)
			if ttl := ttlCmd.Val(); ttl > 0 {
				ttlSeconds = int64((ttl + time.Second - 1) / time.Second)
			}
			out = append(out, ActivePhoneCode{
				Hash:           hash,
				Phone:          code.Phone,
				Email:          code.Email,
				PendingEmail:   code.PendingEmail,
				Code:           code.Code,
				Channel:        code.Channel,
				Purpose:        code.Purpose,
				UserID:         code.UserID,
				IssuedUserID:   code.IssuedUserID,
				Attempts:       code.Attempts,
				MaxAttempts:    code.MaxAttempts,
				SignUpVerified: code.SignUpVerified,
				RequireSignUp:  code.RequireSignUp,
				VerifiedEmail:  code.VerifiedEmail,
				TTLSeconds:     ttlSeconds,
			})
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return out, nil
}
