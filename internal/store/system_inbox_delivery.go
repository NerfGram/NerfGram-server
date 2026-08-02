package store

import (
	"context"

	"telesrv/internal/domain"
)

// SystemInboxDeliveryStore atomically creates a 777000 inbox message with
// dialog projection, user pts, durable update event and dispatch outbox.
// Used for welcome / new-login notices that must reach clients via
// updates.getDifference (plain MessageStore.Create does not allocate pts).
type SystemInboxDeliveryStore interface {
	DeliverSystemInboxMessage(ctx context.Context, msg domain.Message) (domain.Message, error)
}
