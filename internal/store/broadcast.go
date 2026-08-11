package store

import (
	"context"
	"time"

	"telesrv/internal/domain"
)

type BroadcastStore interface {
	PreviewBroadcastRecipients(ctx context.Context, mode domain.BroadcastTargetMode, selectedUserIDs []int64) (int64, error)
	CreateBroadcast(ctx context.Context, message string, mode domain.BroadcastTargetMode, selectedUserIDs []int64, createdBy string) (domain.Broadcast, error)
	MaterializeBroadcastRecipients(ctx context.Context, limit int) (int, error)
	ClaimBroadcastRecipients(ctx context.Context, leaseToken string, limit int, lease time.Duration) ([]BroadcastRecipientClaim, error)
	ReleaseBroadcastRecipient(ctx context.Context, claim BroadcastRecipientClaim, cause string) error
	ListBroadcasts(ctx context.Context, beforeID int64, limit int) ([]domain.Broadcast, bool, error)
}

type BroadcastDeliveryStore interface {
	DeliverBroadcastRecipient(ctx context.Context, claim BroadcastRecipientClaim) (domain.Message, error)
}

type BroadcastRecipientClaim struct {
	RecipientID int64
	BroadcastID int64
	UserID      int64
	Attempts    int
	LeaseToken  string
	Message     string
}
