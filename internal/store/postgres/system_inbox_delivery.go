package postgres

import (
	"context"
	"fmt"
	"time"

	"telesrv/internal/domain"
	"telesrv/internal/store/postgres/sqlcgen"
)

// DeliverSystemInboxMessage commits a 777000 inbox message with dialog, pts,
// durable update event and dispatch outbox — same visibility path as login codes.
func (s *MessageStore) DeliverSystemInboxMessage(ctx context.Context, msg domain.Message) (domain.Message, error) {
	if msg.OwnerUserID <= 0 || domain.IsSystemUserID(msg.OwnerUserID) {
		return domain.Message{}, fmt.Errorf("system inbox: invalid owner %d", msg.OwnerUserID)
	}
	if msg.Peer.Type != domain.PeerTypeUser || msg.Peer.ID != domain.OfficialSystemUserID {
		msg.Peer = domain.Peer{Type: domain.PeerTypeUser, ID: domain.OfficialSystemUserID}
	}
	if msg.From.Type != domain.PeerTypeUser || msg.From.ID != domain.OfficialSystemUserID {
		msg.From = domain.Peer{Type: domain.PeerTypeUser, ID: domain.OfficialSystemUserID}
	}
	if msg.Date == 0 {
		msg.Date = int(time.Now().Unix())
	}
	entitiesJSON, err := encodeMessageEntities(msg.Entities)
	if err != nil {
		return domain.Message{}, fmt.Errorf("encode system inbox entities: %w", err)
	}
	mediaJSON, err := encodeMessageMedia(msg.Media)
	if err != nil {
		return domain.Message{}, fmt.Errorf("encode system inbox media: %w", err)
	}
	replyMarkupJSON, err := encodeReplyMarkup(msg.ReplyMarkup)
	if err != nil {
		return domain.Message{}, fmt.Errorf("encode system inbox reply markup: %w", err)
	}
	richMessageJSON, err := encodeRichMessage(msg.RichMessage)
	if err != nil {
		return domain.Message{}, fmt.Errorf("encode system inbox rich message: %w", err)
	}

	beginner, ok := s.db.(txBeginner)
	if !ok {
		return domain.Message{}, fmt.Errorf("deliver system inbox: database does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return domain.Message{}, fmt.Errorf("begin system inbox delivery: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	if err := lockUsersForUpdate(ctx, tx, msg.OwnerUserID); err != nil {
		return domain.Message{}, fmt.Errorf("lock system inbox recipient: %w", err)
	}
	if err := ensureOfficialSystemUserWithDB(ctx, tx, msg); err != nil {
		return domain.Message{}, err
	}
	qtx := sqlcgen.New(tx)

	pm, err := qtx.CreatePrivateMessage(ctx, sqlcgen.CreatePrivateMessageParams{
		SenderUserID:       domain.OfficialSystemUserID,
		RecipientUserID:    msg.OwnerUserID,
		RandomID:           0,
		MessageDate:        int32(msg.Date),
		Body:               msg.Body,
		RequestFingerprint: []byte{},
		RecipientDelivered: true,
		EntitiesJson:       entitiesJSON,
		QuoteEntitiesJson:  []byte("[]"),
		MediaJson:          mediaJSON,
		ReplyMarkupJson:    replyMarkupJSON,
		RichMessageJson:    richMessageJSON,
		GroupedID:          msg.GroupedID,
		Effect:             msg.Effect,
		Silent:             msg.Silent,
		Noforwards:         msg.NoForwards,
	})
	if err != nil {
		return domain.Message{}, fmt.Errorf("create system inbox private message: %w", err)
	}

	boxID, err := s.nextLoginCodeBoxID(ctx, qtx, msg.OwnerUserID)
	if err != nil {
		return domain.Message{}, fmt.Errorf("allocate system inbox box id: %w", err)
	}
	pts, err := s.reservePts(ctx, tx, msg.OwnerUserID)
	if err != nil {
		return domain.Message{}, fmt.Errorf("allocate system inbox pts: %w", err)
	}

	boxRow, err := qtx.CreateMessageBox(ctx, sqlcgen.CreateMessageBoxParams{
		OwnerUserID:       msg.OwnerUserID,
		BoxID:             int32(boxID),
		PrivateMessageID:  pm.ID,
		MessageSenderID:   domain.OfficialSystemUserID,
		PeerType:          string(domain.PeerTypeUser),
		PeerID:            domain.OfficialSystemUserID,
		FromUserID:        domain.OfficialSystemUserID,
		MessageDate:       int32(msg.Date),
		Outgoing:          false,
		Body:              msg.Body,
		EntitiesJson:      entitiesJSON,
		QuoteEntitiesJson: []byte("[]"),
		Pts:               int32(pts),
		MediaJson:         mediaJSON,
		MediaUnread:       msg.MediaUnread || !msg.Media.IsZero(),
		ReplyMarkupJson:   replyMarkupJSON,
		RichMessageJson:   richMessageJSON,
		GroupedID:         msg.GroupedID,
		Effect:            msg.Effect,
		Silent:            msg.Silent,
		Noforwards:        msg.NoForwards,
	})
	if err != nil {
		return domain.Message{}, fmt.Errorf("create system inbox recipient box: %w", err)
	}
	out := messageFromBoxRow(boxRow)

	if err := qtx.UpsertInboxDialog(ctx, sqlcgen.UpsertInboxDialogParams{
		UserID:         msg.OwnerUserID,
		PeerType:       string(domain.PeerTypeUser),
		PeerID:         domain.OfficialSystemUserID,
		TopMessageID:   int32(out.ID),
		TopMessageDate: int32(out.Date),
	}); err != nil {
		return domain.Message{}, fmt.Errorf("upsert system inbox dialog: %w", err)
	}
	if err := appendNewMessageEvent(ctx, qtx, out); err != nil {
		return domain.Message{}, err
	}
	if err := enqueueDispatch(ctx, qtx, sqlcgen.EnqueueDispatchParams{
		TargetUserID:     msg.OwnerUserID,
		Pts:              int32(out.Pts),
		EventType:        string(domain.UpdateEventNewMessage),
		ExcludeAuthKeyID: 0,
		ExcludeSessionID: 0,
	}); err != nil {
		return domain.Message{}, fmt.Errorf("enqueue system inbox dispatch: %w", err)
	}

	if _, err := tx.Exec(ctx, `
UPDATE private_messages
SET recipient_box_id = $3,
    recipient_pts = $4
WHERE sender_user_id = $1
  AND id = $2
  AND recipient_delivered
  AND recipient_box_id = 0
  AND recipient_pts = 0`, domain.OfficialSystemUserID, pm.ID, out.ID, out.Pts); err != nil {
		return domain.Message{}, fmt.Errorf("save system inbox private receipt: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Message{}, fmt.Errorf("commit system inbox delivery: %w", err)
	}
	committed = true
	return out, nil
}
