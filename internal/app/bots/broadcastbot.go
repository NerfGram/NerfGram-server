package bots

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"telesrv/internal/domain"
)

const (
	broadcastBotHelpText = `I fan out messages to every user as FromGram service notifications.

/start - confirm you have permission to broadcast
/help - show this message

Only support accounts may broadcast; after confirmation, every message you send here (text, media, albums, etc.) is delivered from the service notifications account to all users.`
	broadcastBotVerifiedReady = "You are allowed to broadcast. Send any message and I will deliver it as a service notification."
	broadcastBotEmptyContent  = "Nothing to broadcast."
)

func (s *Service) respondAsBroadcastBot(userID int64, msg domain.Message) {
	mu := s.serviceBotReplyLock(domain.BroadcastBotUserID, userID)
	mu.Lock()
	defer mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s.handleBroadcastBot(ctx, userID, msg)
}

func (s *Service) handleBroadcastBot(ctx context.Context, userID int64, msg domain.Message) {
	// Only support accounts may broadcast.
	u, found, err := s.users.ByID(ctx, userID)
	if err != nil || !found || !u.Support {
		// Inform user about permission only for explicit commands.
		if cmd, ok := parseBotCommand(strings.TrimSpace(msg.Body)); ok && cmd == "help" {
			s.sendServiceBotReply(ctx, domain.BroadcastBotUserID, userID, botReply{Text: broadcastBotHelpText})
			return
		}
		s.sendServiceBotReply(ctx, domain.BroadcastBotUserID, userID, botReply{Text: "Only support accounts can broadcast."})
		return
	}

	// Support account: accept /start and /help, otherwise broadcast.
	if cmd, ok := parseBotCommand(strings.TrimSpace(msg.Body)); ok {
		switch cmd {
		case "start":
			s.sendServiceBotReply(ctx, domain.BroadcastBotUserID, userID, botReply{Text: broadcastBotVerifiedReady})
			return
		case "help":
			s.sendServiceBotReply(ctx, domain.BroadcastBotUserID, userID, botReply{Text: broadcastBotHelpText})
			return
		default:
			s.sendServiceBotReply(ctx, domain.BroadcastBotUserID, userID, botReply{Text: "Unrecognized command. Send /help."})
			return
		}
	}

	if !broadcastMessageHasContent(msg) {
		s.sendServiceBotReply(ctx, domain.BroadcastBotUserID, userID, botReply{Text: broadcastBotEmptyContent})
		return
	}

	sent, failed := s.fanOutBroadcastAsService(ctx, msg)
	ack := fmt.Sprintf("Broadcast sent to %d users.", sent)
	if failed > 0 {
		ack = fmt.Sprintf("Broadcast sent to %d users (%d failed).", sent, failed)
	}
	s.sendServiceBotReply(ctx, domain.BroadcastBotUserID, userID, botReply{Text: ack})
}

func broadcastMessageHasContent(msg domain.Message) bool {
	return strings.TrimSpace(msg.Body) != "" || !msg.Media.IsZero() || !msg.RichMessage.IsZero()
}

func (s *Service) fanOutBroadcastAsService(ctx context.Context, src domain.Message) (sent, failed int) {
	if s.systemInbox == nil {
		s.log.Error("broadcastbot: system inbox delivery store is not configured")
		return 0, 0
	}
	if s.users == nil {
		s.log.Error("broadcastbot: user store is not configured")
		return 0, 0
	}
	recipients, err := s.users.ListBroadcastRecipientIDs(ctx)
	if err != nil {
		s.log.Error("broadcastbot: list recipients", zap.Error(err))
		return 0, 0
	}
	date := int(s.now().Unix())
	entities := append([]domain.MessageEntity(nil), src.Entities...)
	for _, recipientID := range recipients {
		out := domain.Message{
			OwnerUserID: recipientID,
			Peer:        domain.Peer{Type: domain.PeerTypeUser, ID: domain.OfficialSystemUserID},
			From:        domain.Peer{Type: domain.PeerTypeUser, ID: domain.OfficialSystemUserID},
			Date:        date,
			Body:        src.Body,
			Entities:    entities,
			Media:       src.Media,
			MediaUnread: !src.Media.IsZero(),
			Silent:      src.Silent,
			NoForwards:  src.NoForwards,
			GroupedID:   src.GroupedID,
			Effect:      src.Effect,
			RichMessage: src.RichMessage,
		}
		if _, err := s.systemInbox.DeliverSystemInboxMessage(ctx, out); err != nil {
			failed++
			s.log.Warn("broadcastbot: deliver system inbox",
				zap.Int64("recipient_user_id", recipientID),
				zap.Error(err))
			continue
		}
		sent++
	}
	return sent, failed
}
