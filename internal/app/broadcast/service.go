package broadcast

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"telesrv/internal/domain"
	"telesrv/internal/store"
)

type Service struct {
	store    store.BroadcastStore
	delivery store.BroadcastDeliveryStore
	log      *zap.Logger
}

func NewService(st store.BroadcastStore, delivery store.BroadcastDeliveryStore, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{store: st, delivery: delivery, log: log}
}

func (s *Service) Ready() bool {
	return s != nil && s.store != nil && s.delivery != nil
}

func normalizeRequest(message string, mode domain.BroadcastTargetMode, selectedUserIDs []int64) (string, []int64, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", nil, domain.ErrBroadcastMessageEmpty
	}
	if !utf8.ValidString(message) || len(message) > domain.MaxBroadcastMessageBytes {
		return "", nil, domain.ErrBroadcastMessageTooLong
	}
	switch mode {
	case domain.BroadcastTargetAll:
		if len(selectedUserIDs) != 0 {
			return "", nil, domain.ErrBroadcastInvalid
		}
		return message, nil, nil
	case domain.BroadcastTargetSelected:
		if len(selectedUserIDs) == 0 {
			return "", nil, domain.ErrBroadcastNoRecipients
		}
		if len(selectedUserIDs) > domain.MaxBroadcastSelectedRecipients {
			return "", nil, domain.ErrBroadcastInvalid
		}
		seen := make(map[int64]struct{}, len(selectedUserIDs))
		ids := make([]int64, 0, len(selectedUserIDs))
		for _, userID := range selectedUserIDs {
			if userID <= 0 || domain.IsSystemUserID(userID) {
				return "", nil, domain.ErrBroadcastRecipientInvalid
			}
			if _, ok := seen[userID]; ok {
				return "", nil, domain.ErrBroadcastRecipientInvalid
			}
			seen[userID] = struct{}{}
			ids = append(ids, userID)
		}
		return message, ids, nil
	default:
		return "", nil, domain.ErrBroadcastInvalid
	}
}

func (s *Service) Preview(ctx context.Context, message string, mode domain.BroadcastTargetMode, selectedUserIDs []int64) (int64, error) {
	if s == nil || s.store == nil {
		return 0, fmt.Errorf("broadcast store is not configured")
	}
	_, ids, err := normalizeRequest(message, mode, selectedUserIDs)
	if err != nil {
		return 0, err
	}
	return s.store.PreviewBroadcastRecipients(ctx, mode, ids)
}

func (s *Service) Create(ctx context.Context, message string, mode domain.BroadcastTargetMode, selectedUserIDs []int64, createdBy string) (domain.Broadcast, error) {
	if s == nil || s.store == nil {
		return domain.Broadcast{}, fmt.Errorf("broadcast store is not configured")
	}
	message, ids, err := normalizeRequest(message, mode, selectedUserIDs)
	if err != nil {
		return domain.Broadcast{}, err
	}
	return s.store.CreateBroadcast(ctx, message, mode, ids, strings.TrimSpace(createdBy))
}

func (s *Service) List(ctx context.Context, beforeID int64, limit int) ([]domain.Broadcast, bool, error) {
	if s == nil || s.store == nil {
		return nil, false, fmt.Errorf("broadcast store is not configured")
	}
	return s.store.ListBroadcasts(ctx, beforeID, limit)
}

type CycleResult struct {
	Materialized int
	Claimed      int
	Sent         int
	Failed       int
}

func (s *Service) RunCycle(ctx context.Context, leaseToken string, materializeBatch, deliveryBatch int, lease time.Duration) (CycleResult, error) {
	if !s.Ready() {
		return CycleResult{}, nil
	}
	var result CycleResult
	materialized, err := s.store.MaterializeBroadcastRecipients(ctx, materializeBatch)
	if err != nil {
		return result, err
	}
	result.Materialized = materialized
	claims, err := s.store.ClaimBroadcastRecipients(ctx, leaseToken, deliveryBatch, lease)
	if err != nil {
		return result, err
	}
	result.Claimed = len(claims)
	for _, claim := range claims {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if _, err := s.delivery.DeliverBroadcastRecipient(ctx, claim); err != nil {
			result.Failed++
			if releaseErr := s.store.ReleaseBroadcastRecipient(ctx, claim, err.Error()); releaseErr != nil {
				s.log.Warn("release broadcast recipient failed",
					zap.Int64("recipient_id", claim.RecipientID),
					zap.Int64("broadcast_id", claim.BroadcastID),
					zap.Error(releaseErr))
			}
			continue
		}
		result.Sent++
	}
	return result, nil
}
