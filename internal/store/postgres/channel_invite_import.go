package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
	"telesrv/internal/domain"
)

func (s *ChannelStore) CheckInvite(ctx context.Context, userID int64, hash string, date int) (domain.CheckChannelInviteResult, error) {
	if userID == 0 || strings.TrimSpace(hash) == "" {
		return domain.CheckChannelInviteResult{}, domain.ErrInviteHashEmpty
	}
	if date == 0 {
		date = nowUnix()
	}
	channel, invite, err := s.getInviteByHash(ctx, s.db, strings.TrimSpace(hash))
	if err != nil {
		return domain.CheckChannelInviteResult{}, err
	}
	if invite.ExpireDate > 0 && invite.ExpireDate < date {
		return domain.CheckChannelInviteResult{}, domain.ErrInviteHashExpired
	}
	member, err := s.getChannelMember(ctx, s.db, channel.ID, userID)
	already := false
	if err == nil {
		if member.Status == domain.ChannelMemberKicked || member.Status == domain.ChannelMemberBanned || member.BannedRights.ViewMessages {
			return domain.CheckChannelInviteResult{}, domain.ErrInviteHashInvalid
		}
		already = member.Status == domain.ChannelMemberActive
	} else if !errors.Is(err, domain.ErrChannelPrivate) {
		return domain.CheckChannelInviteResult{}, err
	}
	return domain.CheckChannelInviteResult{Channel: channel, Invite: invite, Already: already, Self: member}, nil
}

// ResolvePublicChannelInvite is the anonymous, unauthenticated counterpart of
// CheckInvite: no userID/membership lookup, just "is this hash currently
// joinable" plus the public channel projection for the invite preview page.
func (s *ChannelStore) ResolvePublicChannelInvite(ctx context.Context, hash string) (domain.Channel, domain.ChannelInvite, bool, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return domain.Channel{}, domain.ChannelInvite{}, false, nil
	}
	channel, invite, err := s.getInviteByHash(ctx, s.db, hash)
	if err != nil {
		if errors.Is(err, domain.ErrInviteHashInvalid) {
			return domain.Channel{}, domain.ChannelInvite{}, false, nil
		}
		return domain.Channel{}, domain.ChannelInvite{}, false, err
	}
	if invite.ExpireDate > 0 && invite.ExpireDate < nowUnix() {
		return domain.Channel{}, domain.ChannelInvite{}, false, nil
	}
	return channel, invite, true, nil
}

func (s *ChannelStore) ImportInvite(ctx context.Context, req domain.ImportChannelInviteRequest) (domain.CreateChannelResult, error) {
	if req.UserID == 0 || strings.TrimSpace(req.Hash) == "" {
		return domain.CreateChannelResult{}, domain.ErrInviteHashEmpty
	}
	if req.Date == 0 {
		req.Date = nowUnix()
	}
	beginner, ok := s.db.(txBeginner)
	if !ok {
		return domain.CreateChannelResult{}, fmt.Errorf("import channel invite: db does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return domain.CreateChannelResult{}, fmt.Errorf("begin import channel invite: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	channel, invite, err := s.getInviteByHashForUpdate(ctx, tx, strings.TrimSpace(req.Hash))
	if err != nil {
		return domain.CreateChannelResult{}, err
	}
	if invite.ExpireDate > 0 && invite.ExpireDate < req.Date {
		return domain.CreateChannelResult{}, domain.ErrInviteHashExpired
	}
	if invite.RequestNeeded {
		if err := s.recordPendingInviteImporterTx(ctx, tx, invite, req.UserID, req.Date); err != nil {
			return domain.CreateChannelResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.CreateChannelResult{}, fmt.Errorf("commit pending channel invite request: %w", err)
		}
		committed = true
		return domain.CreateChannelResult{Channel: channel}, domain.ErrInviteRequestSent
	}
	result, err := s.approveInviteImporterTx(ctx, tx, channel, invite, req.UserID, 0, req.Date)
	if err != nil {
		return domain.CreateChannelResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.CreateChannelResult{}, fmt.Errorf("commit import channel invite: %w", err)
	}
	committed = true
	recipients, _ := s.ListActiveChannelMemberIDs(ctx, req.UserID, result.Channel.ID, 0)
	result.Recipients = recipients
	return result, nil
}

func (s *ChannelStore) approveInviteImporterTx(ctx context.Context, tx pgx.Tx, channel domain.Channel, invite domain.ChannelInvite, userID, approvedBy int64, date int) (domain.CreateChannelResult, error) {
	if invite.InviteID != 0 && invite.UsageLimit > 0 && invite.UsageCount >= invite.UsageLimit {
		return domain.CreateChannelResult{}, domain.ErrUsersTooMuch
	}
	channelID := channel.ID
	if channelID == 0 {
		channelID = invite.ChannelID
	}
	if existing, err := s.getChannelMember(ctx, tx, channelID, userID); err == nil {
		if existing.Status == domain.ChannelMemberActive {
			return domain.CreateChannelResult{}, domain.ErrUserAlreadyParticipant
		}
		if existing.Status == domain.ChannelMemberKicked || existing.Status == domain.ChannelMemberBanned || existing.BannedRights.ViewMessages {
			return domain.CreateChannelResult{}, domain.ErrInviteHashInvalid
		}
	} else if !errors.Is(err, domain.ErrChannelPrivate) {
		return domain.CreateChannelResult{}, err
	}
	if err := s.chargeStarsSubscriptionTx(ctx, tx, channel, invite, userID, date); err != nil {
		return domain.CreateChannelResult{}, err
	}
	preJoinTopID := channel.TopMessageID
	minID := channelInitialAvailableMinID(channel)
	inviterID := invite.AdminUserID
	if inviterID == 0 {
		inviterID = approvedBy
	}
	member := domain.ChannelMember{
		ChannelID:       channelID,
		UserID:          userID,
		InviterUserID:   inviterID,
		Role:            domain.ChannelRoleMember,
		Status:          domain.ChannelMemberActive,
		JoinedAt:        date,
		AvailableMinID:  minID,
		AvailableMinPts: channelInitialAvailableMinPts(channel),
		ReadInboxMaxID:  maxInt(minID, preJoinTopID),
	}
	if err := upsertChannelMemberTx(ctx, tx, channel, member); err != nil {
		return domain.CreateChannelResult{}, err
	}
	if err := s.insertChannelAdminLogTx(ctx, tx, domain.ChannelAdminLogEvent{
		ChannelID: channelID,
		UserID:    userID,
		Date:      date,
		Type:      domain.ChannelAdminLogParticipantJoin,
	}); err != nil {
		return domain.CreateChannelResult{}, err
	}
	if invite.InviteID != 0 {
		if _, err := tx.Exec(ctx, `
UPDATE channel_invites
SET requested_count = GREATEST(requested_count - 1, 0),
    updated_at = now()
WHERE channel_id = $1
  AND invite_id = (
      SELECT invite_id
      FROM channel_invite_importers
      WHERE channel_id = $1 AND user_id = $2 AND requested
  )`, channelID, userID); err != nil {
			return domain.CreateChannelResult{}, fmt.Errorf("clear pending channel invite request: %w", err)
		}
		if _, err := tx.Exec(ctx, `
UPDATE channel_invites
SET usage_count = usage_count + 1,
    updated_at = now()
WHERE channel_id = $1 AND invite_id = $2`, channelID, invite.InviteID); err != nil {
			return domain.CreateChannelResult{}, fmt.Errorf("increment channel invite usage: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO channel_invite_importers (channel_id, invite_id, user_id, date, requested, approved_by)
VALUES ($1, $2, $3, $4, false, $5)
ON CONFLICT (channel_id, user_id) DO UPDATE
SET invite_id = EXCLUDED.invite_id,
    date = EXCLUDED.date,
    requested = false,
    approved_by = EXCLUDED.approved_by,
    updated_at = now()`, channelID, invite.InviteID, userID, date, approvedBy); err != nil {
		return domain.CreateChannelResult{}, fmt.Errorf("upsert channel invite importer: %w", err)
	}
	channel, err := refreshChannelCountsTx(ctx, tx, channel)
	if err != nil {
		return domain.CreateChannelResult{}, err
	}
	var msg domain.ChannelMessage
	var event domain.ChannelUpdateEvent
	if channel.Megagroup {
		msg, event, err = s.insertServiceMessage(ctx, tx, channel, userID, date, domain.ChannelMessageAction{
			Type:          domain.ChannelActionChatJoinedByLink,
			InviterUserID: invite.AdminUserID,
			UserIDs:       []int64{userID},
		})
		if err != nil {
			return domain.CreateChannelResult{}, err
		}
		channel.TopMessageID = msg.ID
		channel.Pts = event.Pts
	}
	member.ReadInboxMaxID = maxInt(member.ReadInboxMaxID, channel.TopMessageID)
	if msg.ID != 0 {
		member.ReadOutboxMaxID = maxInt(member.ReadOutboxMaxID, msg.ID)
	}
	if _, err := tx.Exec(ctx, `
UPDATE channel_members
SET read_inbox_max_id = $3, read_outbox_max_id = $4, updated_at = now()
WHERE channel_id = $1 AND user_id = $2`, channel.ID, userID, member.ReadInboxMaxID, member.ReadOutboxMaxID); err != nil {
		return domain.CreateChannelResult{}, fmt.Errorf("update imported member read state: %w", err)
	}
	if err := upsertChannelDialogTx(ctx, tx, userID, channel, msg, member.ReadInboxMaxID, member.ReadOutboxMaxID); err != nil {
		return domain.CreateChannelResult{}, err
	}
	// 经邀请链接重新入群同样是重进:按新 available_min_id 重算未读 reaction 计数,
	// 清掉离群期间残留的幽灵角标(同 JoinChannel,门控自动对 prehistory-visible 保留真实计数)。
	if err := refreshChannelUnreadReactionsCountTx(ctx, tx, userID, channel.ID); err != nil {
		return domain.CreateChannelResult{}, err
	}
	return domain.CreateChannelResult{Channel: channel, Members: []domain.ChannelMember{member}, Message: msg, Event: event}, nil
}

func (s *ChannelStore) recordPendingInviteImporterTx(ctx context.Context, tx pgx.Tx, invite domain.ChannelInvite, userID int64, date int) error {
	if existing, err := s.getChannelMember(ctx, tx, invite.ChannelID, userID); err == nil {
		if existing.Status == domain.ChannelMemberActive {
			return domain.ErrUserAlreadyParticipant
		}
		if existing.Status == domain.ChannelMemberKicked || existing.Status == domain.ChannelMemberBanned || existing.BannedRights.ViewMessages {
			return domain.ErrInviteHashInvalid
		}
	} else if !errors.Is(err, domain.ErrChannelPrivate) {
		return err
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO channel_invite_importers (channel_id, invite_id, user_id, date, requested)
VALUES ($1, $2, $3, $4, true)
ON CONFLICT (channel_id, user_id) DO UPDATE
SET invite_id = EXCLUDED.invite_id,
    date = EXCLUDED.date,
    requested = true,
    approved_by = 0,
    updated_at = now()
WHERE NOT channel_invite_importers.requested`,
		invite.ChannelID, invite.InviteID, userID, date)
	if err != nil {
		return fmt.Errorf("record pending channel invite importer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrInviteRequestSent
	}
	if _, err := tx.Exec(ctx, `
UPDATE channel_invites
SET requested_count = requested_count + 1, updated_at = now()
WHERE channel_id = $1 AND invite_id = $2`, invite.ChannelID, invite.InviteID); err != nil {
		return fmt.Errorf("increment channel invite requested count: %w", err)
	}
	return nil
}

func (s *ChannelStore) getPendingInviteImporterTx(ctx context.Context, tx pgx.Tx, channelID, userID int64, forUpdate bool) (domain.ChannelInviteImporter, error) {
	lockClause := ""
	if forUpdate {
		lockClause = " FOR UPDATE"
	}
	row := tx.QueryRow(ctx, `
SELECT channel_id, invite_id, user_id, date, requested, approved_by, via_chatlist, about
FROM channel_invite_importers
WHERE channel_id = $1 AND user_id = $2 AND requested`+lockClause, channelID, userID)
	var importer domain.ChannelInviteImporter
	err := row.Scan(&importer.ChannelID, &importer.InviteID, &importer.UserID, &importer.Date, &importer.Requested, &importer.ApprovedBy, &importer.ViaChatlist, &importer.About)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ChannelInviteImporter{}, domain.ErrHideRequesterMissing
	}
	return importer, err
}

func deletePendingInviteImporterTx(ctx context.Context, tx pgx.Tx, invite domain.ChannelInvite, userID int64) error {
	tag, err := tx.Exec(ctx, `
DELETE FROM channel_invite_importers
WHERE channel_id = $1 AND user_id = $2 AND requested`, invite.ChannelID, userID)
	if err != nil {
		return fmt.Errorf("delete pending channel invite importer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrHideRequesterMissing
	}
	if invite.InviteID == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
UPDATE channel_invites
SET requested_count = CASE WHEN requested_count > 0 THEN requested_count - 1 ELSE 0 END,
    updated_at = now()
WHERE channel_id = $1 AND invite_id = $2`, invite.ChannelID, invite.InviteID); err != nil {
		return fmt.Errorf("decrement channel invite requested count: %w", err)
	}
	return nil
}

func (s *ChannelStore) chargeStarsSubscriptionTx(ctx context.Context, tx pgx.Tx, channel domain.Channel, invite domain.ChannelInvite, userID int64, date int) error {
	if !invite.HasStarsSubscription() {
		return nil
	}
	channelID := channel.ID
	if channelID == 0 {
		channelID = invite.ChannelID
	}
	var existingUntil int
	err := tx.QueryRow(ctx, `
SELECT until_date
FROM stars_subscriptions
WHERE user_id = $1 AND channel_id = $2 AND NOT canceled`, userID, channelID).Scan(&existingUntil)
	if err == nil && existingUntil > date {
		return nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lookup stars subscription: %w", err)
	}
	balance := domain.StarsBalance{UserID: userID}
	if err := tx.QueryRow(ctx, `SELECT balance, granted FROM stars_balances WHERE user_id = $1 FOR UPDATE`, userID).
		Scan(&balance.Balance, &balance.Granted); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrStarsInsufficient
		}
		return fmt.Errorf("lock subscription payer balance: %w", err)
	}
	if balance.Balance < invite.SubscriptionAmount {
		return domain.ErrStarsInsufficient
	}
	if err := tx.QueryRow(ctx, `
UPDATE stars_balances
SET balance = balance - $2, updated_at = now()
WHERE user_id = $1
RETURNING balance`, userID, invite.SubscriptionAmount).Scan(&balance.Balance); err != nil {
		return fmt.Errorf("debit subscription payer balance: %w", err)
	}
	title := channel.Title
	if title == "" {
		title = "Channel subscription"
	}
	if err := insertStarsTxn(ctx, tx, userID, -invite.SubscriptionAmount, domain.StarsReasonSubscription,
		domain.Peer{Type: domain.PeerTypeChannel, ID: channelID}, date, title, ""); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO channel_stars_balances(channel_id, balance)
VALUES($1, $2)
ON CONFLICT(channel_id) DO UPDATE
SET balance = channel_stars_balances.balance + EXCLUDED.balance, updated_at = now()`,
		channelID, invite.SubscriptionAmount); err != nil {
		return fmt.Errorf("credit channel stars for subscription: %w", err)
	}
	until := date + invite.SubscriptionPeriod
	if _, err := tx.Exec(ctx, `
INSERT INTO stars_subscriptions (
    user_id, channel_id, invite_hash, until_date, period, amount, canceled, title, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,false,$7,now(),now())
ON CONFLICT (user_id, channel_id) DO UPDATE SET
    invite_hash = EXCLUDED.invite_hash,
    until_date = EXCLUDED.until_date,
    period = EXCLUDED.period,
    amount = EXCLUDED.amount,
    canceled = false,
    title = EXCLUDED.title,
    updated_at = now()`,
		userID, channelID, invite.Hash, until, invite.SubscriptionPeriod, invite.SubscriptionAmount, title); err != nil {
		return fmt.Errorf("upsert stars subscription: %w", err)
	}
	return nil
}
