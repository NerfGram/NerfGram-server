package bots

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"telesrv/internal/domain"
	"telesrv/internal/links"
)

const starsTestBotHelp = `FromGram Stars test bot

Commands:
/grant [amount] — credit Stars to your balance (default 100)
/paidlink <channel_id> <amount> — export a monthly Stars invite for a channel you admin

Then open the link on another account (or in an incognito session) to join and charge Stars.
Active subscriptions show up in payments.getStarsSubscriptions.`

type inviteExporter interface {
	ExportInvite(ctx context.Context, userID int64, req domain.ExportChannelInviteRequest) (domain.ExportChannelInviteResult, error)
}

type starsLedger interface {
	GetBalance(ctx context.Context, userID int64) (domain.StarsBalance, error)
	Credit(ctx context.Context, userID, amount int64, reason domain.StarsTransactionReason, peer domain.Peer, title, desc string) (domain.StarsBalance, error)
}

// SetStarsTestDeps wires paid-invite helpers used by @StarsTestBot.
// Call after channels + stars services exist (breaks construction order cycles).
func (s *Service) SetStarsTestDeps(invites inviteExporter, stars starsLedger) {
	if s == nil {
		return
	}
	s.inviteExporter = invites
	s.starsLedger = stars
}

func (s *Service) respondAsStarsTestBot(userID int64, body string) {
	mu := s.serviceBotReplyLock(domain.StarsTestBotUserID, userID)
	mu.Lock()
	defer mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s.sendServiceBotReply(ctx, domain.StarsTestBotUserID, userID, s.handleStarsTestBot(ctx, userID, body))
}

func (s *Service) handleStarsTestBot(ctx context.Context, userID int64, body string) botReply {
	cmd, args := parseStarsTestCommand(body)
	switch cmd {
	case "", "/start", "/help":
		return botReply{Text: starsTestBotHelp}
	case "/grant":
		return s.starsTestGrant(ctx, userID, args)
	case "/paidlink":
		return s.starsTestPaidLink(ctx, userID, args)
	default:
		return botReply{Text: "Unknown command. Send /help."}
	}
}

func (s *Service) starsTestGrant(ctx context.Context, userID int64, args string) botReply {
	if s.starsLedger == nil {
		return botReply{Text: "Stars ledger is not configured."}
	}
	amount := int64(100)
	if args != "" {
		parsed, err := strconv.ParseInt(args, 10, 64)
		if err != nil || parsed <= 0 {
			return botReply{Text: "Usage: /grant [amount]"}
		}
		amount = parsed
	}
	bal, err := s.starsLedger.Credit(ctx, userID, amount, domain.StarsReasonAdjust, domain.Peer{}, "StarsTestBot grant", "")
	if err != nil {
		return botReply{Text: "Could not credit Stars: " + err.Error()}
	}
	return botReply{Text: fmt.Sprintf("Credited %d Stars. Balance: %d.", amount, bal.Balance)}
}

func (s *Service) starsTestPaidLink(ctx context.Context, userID int64, args string) botReply {
	if s.inviteExporter == nil {
		return botReply{Text: "Invite export is not configured."}
	}
	parts := strings.Fields(args)
	if len(parts) != 2 {
		return botReply{Text: "Usage: /paidlink <channel_id> <amount>"}
	}
	channelID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || channelID == 0 {
		return botReply{Text: "Invalid channel_id."}
	}
	amount, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || amount <= 0 {
		return botReply{Text: "Invalid amount."}
	}
	period, amount, err := domain.NormalizeStarsSubscriptionPricing(domain.DefaultStarsSubscriptionPeriod, amount)
	if err != nil {
		return botReply{Text: err.Error()}
	}
	res, err := s.inviteExporter.ExportInvite(ctx, userID, domain.ExportChannelInviteRequest{
		UserID:             userID,
		ChannelID:          channelID,
		Title:              "Stars subscription",
		Date:               int(time.Now().Unix()),
		SubscriptionPeriod: period,
		SubscriptionAmount: amount,
	})
	if err != nil {
		return botReply{Text: "Could not export invite: " + err.Error()}
	}
	link := links.NormalizeBaseURL(s.publicBaseURL)
	if link == "" {
		link = "https://fromgram.org"
	}
	link = strings.TrimRight(link, "/") + "/+" + res.Invite.Hash
	return botReply{Text: fmt.Sprintf("Paid invite ready (%d Stars / 30 days):\n%s", amount, link)}
}

func parseStarsTestCommand(body string) (cmd, args string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", ""
	}
	parts := strings.Fields(body)
	cmd = strings.ToLower(parts[0])
	if at := strings.IndexByte(cmd, '@'); at > 0 {
		cmd = cmd[:at]
	}
	if len(parts) > 1 {
		args = strings.TrimSpace(strings.TrimPrefix(body, parts[0]))
	}
	return cmd, args
}
