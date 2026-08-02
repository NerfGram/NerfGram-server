package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"telesrv/internal/domain"
)

var errPrivateNoForwardsNoop = errors.New("private no forwards no-op")

func pgNoForwardsPair(a, b int64) (low, high int64, ok bool) {
	if a <= 0 || b <= 0 || a == b {
		return 0, 0, false
	}
	if a > b {
		a, b = b, a
	}
	return a, b, true
}

func (s *MessageStore) GetPrivateNoForwards(ctx context.Context, viewerUserID, peerUserID int64) (domain.PrivateNoForwardsState, error) {
	low, high, ok := pgNoForwardsPair(viewerUserID, peerUserID)
	if !ok {
		return domain.PrivateNoForwardsState{}, domain.ErrMessageIDInvalid
	}
	state := domain.PrivateNoForwardsState{UserLowID: low, UserHighID: high}
	err := s.db.QueryRow(ctx, `
SELECT COALESCE(enabled_by_user_id, 0)
FROM private_no_forwards_chats
WHERE user_low_id = $1 AND user_high_id = $2`, low, high).Scan(&state.EnabledByUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return domain.PrivateNoForwardsState{}, fmt.Errorf("get private no forwards: %w", err)
	}
	return state, nil
}

func (s *MessageStore) TogglePrivateNoForwards(ctx context.Context, req domain.TogglePrivateNoForwardsRequest) (domain.TogglePrivateNoForwardsResult, error) {
	low, high, ok := pgNoForwardsPair(req.ActorUserID, req.PeerUserID)
	if !ok || req.RequestMsgID < 0 || req.RequestMsgID > domain.MaxMessageBoxID {
		return domain.TogglePrivateNoForwardsResult{}, domain.ErrMessageIDInvalid
	}
	if req.Date == 0 {
		req.Date = int(time.Now().Unix())
	}
	if req.RandomID == 0 {
		req.RandomID = time.Now().UnixNano()
		if req.RandomID == 0 {
			req.RandomID = 1
		}
	}

	state := domain.PrivateNoForwardsState{UserLowID: low, UserHighID: high}
	actionKind := domain.MessageServiceActionNoForwardsToggle
	action := domain.MessageNoForwardsAction{}
	var answeredRequestSenderID, answeredRequestMessageID int64

	sendReq := domain.SendPrivateTextRequest{
		SenderUserID:    req.ActorUserID,
		RecipientUserID: req.PeerUserID,
		RandomID:        req.RandomID,
		Silent:          true,
		Date:            req.Date,
		OriginAuthKeyID: req.OriginAuthKeyID,
		OriginSessionID: req.OriginSessionID,
		// A non-empty placeholder is required before the send transaction starts.
		// The pair-locked before-hook replaces it with the authoritative action.
		Media: noForwardsServiceMedia(actionKind, action),
	}
	if req.RequestMsgID != 0 {
		sendReq.ReplyTo = &domain.MessageReply{
			MessageID: req.RequestMsgID,
			Peer:      domain.Peer{Type: domain.PeerTypeUser, ID: req.PeerUserID},
		}
	}

	hooks := privateSendTxHooks{
		before: func(ctx context.Context, tx pgx.Tx, send *domain.SendPrivateTextRequest) error {
			if _, err := tx.Exec(ctx, `
INSERT INTO private_no_forwards_chats (user_low_id, user_high_id)
VALUES ($1, $2)
ON CONFLICT (user_low_id, user_high_id) DO NOTHING`, low, high); err != nil {
				return fmt.Errorf("ensure private no forwards state: %w", err)
			}
			if err := tx.QueryRow(ctx, `
SELECT COALESCE(enabled_by_user_id, 0)
FROM private_no_forwards_chats
WHERE user_low_id = $1 AND user_high_id = $2
FOR UPDATE`, low, high).Scan(&state.EnabledByUserID); err != nil {
				return fmt.Errorf("lock private no forwards state: %w", err)
			}

			previousEnabled := state.Enabled()
			actionKind = domain.MessageServiceActionNoForwardsToggle
			action = domain.MessageNoForwardsAction{}
			if req.RequestMsgID != 0 {
				var expiresAt, handledAt int
				err := tx.QueryRow(ctx, `
SELECT r.private_message_sender_user_id, r.private_message_id, r.expires_at, r.handled_at
FROM message_boxes AS b
JOIN private_no_forwards_requests AS r
  ON r.private_message_sender_user_id = b.message_sender_id
 AND r.private_message_id = b.private_message_id
WHERE b.owner_user_id = $1
  AND b.box_id = $2
  AND b.peer_type = 'user'
  AND b.peer_id = $3
  AND r.requester_user_id = $3
  AND r.responder_user_id = $1
FOR UPDATE OF r`,
					req.ActorUserID, req.RequestMsgID, req.PeerUserID,
				).Scan(&answeredRequestSenderID, &answeredRequestMessageID, &expiresAt, &handledAt)
				if errors.Is(err, pgx.ErrNoRows) {
					return domain.ErrNoForwardsRequestExpired
				}
				if err != nil {
					return fmt.Errorf("lock private no forwards request: %w", err)
				}
				if handledAt != 0 || expiresAt <= req.Date {
					return domain.ErrNoForwardsRequestExpired
				}
				action = domain.MessageNoForwardsAction{PrevValue: previousEnabled, NewValue: req.Enabled}
				if req.Enabled {
					state.EnabledByUserID = req.ActorUserID
				} else {
					state.EnabledByUserID = 0
				}
				if _, err := tx.Exec(ctx, `
UPDATE private_no_forwards_requests
SET handled_at = $3
WHERE private_message_sender_user_id = $1
  AND private_message_id = $2
  AND handled_at = 0`,
					answeredRequestSenderID, answeredRequestMessageID, req.Date,
				); err != nil {
					return fmt.Errorf("handle private no forwards request: %w", err)
				}
				if err := expirePGNoForwardsRequest(ctx, tx, answeredRequestSenderID, answeredRequestMessageID); err != nil {
					return err
				}
			} else if req.Enabled {
				if state.EnabledByUserID != 0 {
					return errPrivateNoForwardsNoop
				}
				action = domain.MessageNoForwardsAction{PrevValue: false, NewValue: true}
				state.EnabledByUserID = req.ActorUserID
			} else {
				switch state.EnabledByUserID {
				case 0:
					return errPrivateNoForwardsNoop
				case req.ActorUserID:
					action = domain.MessageNoForwardsAction{PrevValue: true, NewValue: false}
					state.EnabledByUserID = 0
				default:
					actionKind = domain.MessageServiceActionNoForwardsRequest
					action = domain.MessageNoForwardsAction{
						PrevValue: true,
						NewValue:  false,
						ExpiresAt: req.Date + domain.PrivateNoForwardsRequestExpirePeriod,
					}
				}
			}
			var enabledBy any
			if state.EnabledByUserID != 0 {
				enabledBy = state.EnabledByUserID
			}
			if _, err := tx.Exec(ctx, `
UPDATE private_no_forwards_chats
SET enabled_by_user_id = $3, updated_at = now()
WHERE user_low_id = $1 AND user_high_id = $2`, low, high, enabledBy); err != nil {
				return fmt.Errorf("update private no forwards state: %w", err)
			}
			send.Media = noForwardsServiceMedia(actionKind, action)
			return nil
		},
		after: func(ctx context.Context, tx pgx.Tx, result domain.SendPrivateTextResult) error {
			if actionKind != domain.MessageServiceActionNoForwardsRequest {
				return nil
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO private_no_forwards_requests (
    private_message_sender_user_id,
    private_message_id,
    requester_user_id,
    responder_user_id,
    expires_at
) VALUES ($1, $2, $3, $4, $5)`,
				req.ActorUserID, result.SenderMessage.UID, req.ActorUserID, req.PeerUserID, action.ExpiresAt,
			); err != nil {
				return fmt.Errorf("create private no forwards request: %w", err)
			}
			return nil
		},
	}
	send, err := s.sendPrivateTextWithHooks(ctx, sendReq, hooks)
	if errors.Is(err, errPrivateNoForwardsNoop) {
		return domain.TogglePrivateNoForwardsResult{State: state}, nil
	}
	if errors.Is(err, domain.ErrReplyMessageIDInvalid) {
		return domain.TogglePrivateNoForwardsResult{}, domain.ErrNoForwardsRequestExpired
	}
	if err != nil {
		return domain.TogglePrivateNoForwardsResult{}, err
	}
	return domain.TogglePrivateNoForwardsResult{State: state, Changed: true, Send: send}, nil
}

func noForwardsServiceMedia(kind domain.MessageServiceActionKind, action domain.MessageNoForwardsAction) *domain.MessageMedia {
	return &domain.MessageMedia{
		Kind: domain.MessageMediaKindService,
		ServiceAction: &domain.MessageServiceAction{
			Kind:       kind,
			NoForwards: &action,
		},
	}
}

func expirePGNoForwardsRequest(ctx context.Context, tx pgx.Tx, senderUserID, privateMessageID int64) error {
	for _, statement := range []string{
		`UPDATE private_messages
SET media = jsonb_set(media, '{service_action,no_forwards,expired}', 'true'::jsonb, true)
WHERE sender_user_id = $1 AND id = $2`,
		`UPDATE message_boxes
SET media = jsonb_set(media, '{service_action,no_forwards,expired}', 'true'::jsonb, true)
WHERE message_sender_id = $1 AND private_message_id = $2`,
	} {
		tag, err := tx.Exec(ctx, statement, senderUserID, privateMessageID)
		if err != nil {
			return fmt.Errorf("expire private no forwards request: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("expire private no forwards request: message disappeared")
		}
	}
	return nil
}

func (s *MessageStore) privateNoForwardsEnabled(ctx context.Context, a, b int64) (bool, error) {
	state, err := s.GetPrivateNoForwards(ctx, a, b)
	return state.Enabled(), err
}
