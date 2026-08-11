package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"telesrv/internal/domain"
	"telesrv/internal/store"
	"telesrv/internal/store/postgres/sqlcgen"
)

var _ store.BroadcastDeliveryStore = (*MessageStore)(nil)

// DeliverBroadcastRecipient turns one claimed recipient into one incoming-only
// 777000 message. The recipient receipt is closed in the same transaction as
// the message box, PTS event and dispatch outbox row.
func (s *MessageStore) DeliverBroadcastRecipient(ctx context.Context, claim store.BroadcastRecipientClaim) (domain.Message, error) {
	if claim.RecipientID <= 0 || claim.BroadcastID <= 0 || claim.UserID <= 0 || claim.LeaseToken == "" {
		return domain.Message{}, domain.ErrBroadcastInvalid
	}
	beginner, ok := s.db.(txBeginner)
	if !ok {
		return domain.Message{}, fmt.Errorf("deliver broadcast recipient: database does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return domain.Message{}, fmt.Errorf("begin broadcast delivery: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var userID int64
	var message, status, leaseToken string
	if err := tx.QueryRow(ctx, `
SELECT r.user_id, b.message, r.status, r.lease_token
FROM broadcast_recipients r
JOIN broadcasts b ON b.id = r.broadcast_id
WHERE r.id = $1 AND r.broadcast_id = $2
FOR UPDATE OF r`, claim.RecipientID, claim.BroadcastID).Scan(&userID, &message, &status, &leaseToken); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Message{}, domain.ErrBroadcastLeaseLost
		}
		return domain.Message{}, fmt.Errorf("lock broadcast recipient: %w", err)
	}
	if userID != claim.UserID || status != string(domain.BroadcastRecipientProcessing) || leaseToken != claim.LeaseToken {
		return domain.Message{}, domain.ErrBroadcastLeaseLost
	}

	if err := lockUsersForUpdate(ctx, tx, userID); err != nil {
		return domain.Message{}, fmt.Errorf("lock broadcast recipient user: %w", err)
	}
	var eligible bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM users
  WHERE id = $1 AND NOT is_bot AND deleted_at IS NULL
)`, userID).Scan(&eligible); err != nil {
		return domain.Message{}, fmt.Errorf("check broadcast recipient: %w", err)
	}
	if !eligible || domain.IsSystemUserID(userID) {
		return domain.Message{}, domain.ErrBroadcastRecipientInvalid
	}
	date := int(time.Now().Unix())
	base := domain.Message{
		OwnerUserID: userID,
		Peer:        domain.Peer{Type: domain.PeerTypeUser, ID: domain.OfficialSystemUserID},
		From:        domain.Peer{Type: domain.PeerTypeUser, ID: domain.OfficialSystemUserID},
		Date:        date,
		Body:        message,
	}
	if err := ensureOfficialSystemUserWithDB(ctx, tx, base); err != nil {
		return domain.Message{}, err
	}
	qtx := sqlcgen.New(tx)
	pm, err := qtx.CreatePrivateMessage(ctx, sqlcgen.CreatePrivateMessageParams{
		SenderUserID:       domain.OfficialSystemUserID,
		RecipientUserID:    userID,
		RandomID:           0,
		RequestFingerprint: []byte{},
		RecipientDelivered: true,
		MessageDate:        int32(date),
		Body:               message,
		EntitiesJson:       []byte("[]"),
		QuoteEntitiesJson:  []byte("[]"),
		MediaJson:          []byte("{}"),
		ReplyMarkupJson:    []byte("{}"),
		RichMessageJson:    []byte("{}"),
	})
	if err != nil {
		return domain.Message{}, fmt.Errorf("create broadcast private message: %w", err)
	}
	boxID, err := s.nextIncomingSystemBoxID(ctx, qtx, userID)
	if err != nil {
		return domain.Message{}, fmt.Errorf("allocate broadcast box id: %w", err)
	}
	if boxID <= 0 || boxID > domain.MaxMessageBoxID {
		return domain.Message{}, fmt.Errorf("allocate broadcast box id: %d", boxID)
	}
	pts, err := s.reservePts(ctx, tx, userID)
	if err != nil {
		return domain.Message{}, fmt.Errorf("allocate broadcast pts: %w", err)
	}
	boxRow, err := qtx.CreateMessageBox(ctx, sqlcgen.CreateMessageBoxParams{
		OwnerUserID:       userID,
		BoxID:             int32(boxID),
		PrivateMessageID:  pm.ID,
		MessageSenderID:   domain.OfficialSystemUserID,
		PeerType:          string(domain.PeerTypeUser),
		PeerID:            domain.OfficialSystemUserID,
		FromUserID:        domain.OfficialSystemUserID,
		MessageDate:       int32(date),
		Outgoing:          false,
		Body:              message,
		EntitiesJson:      []byte("[]"),
		QuoteEntitiesJson: []byte("[]"),
		Pts:               int32(pts),
		MediaJson:         []byte("{}"),
		ReplyMarkupJson:   []byte("{}"),
		RichMessageJson:   []byte("{}"),
	})
	if err != nil {
		return domain.Message{}, fmt.Errorf("create broadcast recipient box: %w", err)
	}
	msg := messageFromBoxRow(boxRow)
	if err := qtx.UpsertInboxDialog(ctx, sqlcgen.UpsertInboxDialogParams{
		UserID:         userID,
		PeerType:       string(domain.PeerTypeUser),
		PeerID:         domain.OfficialSystemUserID,
		TopMessageID:   int32(msg.ID),
		TopMessageDate: int32(msg.Date),
	}); err != nil {
		return domain.Message{}, fmt.Errorf("upsert broadcast dialog: %w", err)
	}
	if err := appendNewMessageEvent(ctx, qtx, msg); err != nil {
		return domain.Message{}, err
	}
	if err := enqueueDispatch(ctx, qtx, sqlcgen.EnqueueDispatchParams{
		TargetUserID:     userID,
		Pts:              int32(msg.Pts),
		EventType:        string(domain.UpdateEventNewMessage),
		ExcludeAuthKeyID: 0,
		ExcludeSessionID: 0,
	}); err != nil {
		return domain.Message{}, fmt.Errorf("enqueue broadcast dispatch: %w", err)
	}
	if tag, err := tx.Exec(ctx, `
UPDATE private_messages
SET recipient_box_id = $3, recipient_pts = $4
WHERE sender_user_id = $1 AND id = $2
  AND recipient_delivered
  AND recipient_box_id = 0 AND recipient_pts = 0`,
		domain.OfficialSystemUserID, pm.ID, msg.ID, msg.Pts); err != nil {
		return domain.Message{}, fmt.Errorf("save broadcast private receipt: %w", err)
	} else if tag.RowsAffected() != 1 {
		return domain.Message{}, fmt.Errorf("save broadcast private receipt: logical message disappeared")
	}
	if tag, err := tx.Exec(ctx, `
UPDATE broadcast_recipients
SET status = 'sent', lease_token = '', lease_until = NULL,
    last_error = '', private_message_id = $3, message_box_id = $4,
    pts = $5, sent_at = now(), updated_at = now()
WHERE id = $1 AND status = 'processing' AND lease_token = $2`,
		claim.RecipientID, claim.LeaseToken, msg.UID, msg.ID, msg.Pts); err != nil {
		return domain.Message{}, fmt.Errorf("complete broadcast recipient: %w", err)
	} else if tag.RowsAffected() != 1 {
		return domain.Message{}, domain.ErrBroadcastLeaseLost
	}
	if tag, err := tx.Exec(ctx, `
UPDATE broadcasts SET sent_count = sent_count + 1 WHERE id = $1`, claim.BroadcastID); err != nil {
		return domain.Message{}, fmt.Errorf("advance broadcast sent count: %w", err)
	} else if tag.RowsAffected() != 1 {
		return domain.Message{}, fmt.Errorf("advance broadcast sent count: campaign disappeared")
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Message{}, fmt.Errorf("commit broadcast delivery: %w", err)
	}
	committed = true
	return msg, nil
}
