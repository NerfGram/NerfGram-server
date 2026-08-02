package store

import (
	"context"

	"telesrv/internal/domain"
)

// PrivateNoForwardsStore keeps the canonical pair state and its service-message
// transition in one store transaction. It is optional so unrelated lightweight
// MessageStore test doubles do not need to implement this capability.
type PrivateNoForwardsStore interface {
	GetPrivateNoForwards(ctx context.Context, viewerUserID, peerUserID int64) (domain.PrivateNoForwardsState, error)
	TogglePrivateNoForwards(ctx context.Context, req domain.TogglePrivateNoForwardsRequest) (domain.TogglePrivateNoForwardsResult, error)
}
