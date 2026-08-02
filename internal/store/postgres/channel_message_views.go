package postgres

import (
	"context"
	"fmt"

	"telesrv/internal/domain"
)

type channelMessageViewSummary struct {
	ID         int
	Views      int
	Post       bool
	Discussion *domain.ChannelDiscussionRef
	Sender     domain.Peer
}

type channelMessageViewReplyRef struct {
	messageID int
	replies   *domain.ChannelMessageReplies
}

func (s *ChannelStore) GetChannelMessageViews(ctx context.Context, req domain.ChannelMessageViewsRequest) (domain.ChannelMessageViewsResult, error) {
	if req.UserID == 0 || req.ChannelID == 0 {
		return domain.ChannelMessageViewsResult{}, domain.ErrChannelInvalid
	}
	channel, member, _, err := s.getChannelForViewer(ctx, s.db, req.UserID, req.ChannelID)
	if err != nil {
		return domain.ChannelMessageViewsResult{}, err
	}
	if len(req.IDs) == 0 {
		return domain.ChannelMessageViewsResult{Channel: channel, Views: map[int]int{}, Replies: map[int]*domain.ChannelMessageReplies{}}, nil
	}
	if len(req.IDs) > domain.MaxGetMessageIDs {
		return domain.ChannelMessageViewsResult{}, domain.ErrChannelInvalid
	}
	id32, _, err := validUniqueChannelMessageIDs(req.IDs)
	if err != nil {
		return domain.ChannelMessageViewsResult{}, err
	}
	if req.Increment {
		date := req.Date
		if date <= 0 {
			date = nowUnix()
		}
		args := []any{req.ChannelID, id32, req.UserID, date, member.AvailableMinID}
		visibility := ""
		if channel.Monoforum && !member.CanManageDirectMessages() {
			args = append(args, string(domain.PeerTypeUser), req.UserID)
			visibility = fmt.Sprintf(
				" AND m.saved_peer_type = $%d AND m.saved_peer_id = $%d",
				len(args)-1,
				len(args),
			)
		}
		rows, err := s.db.Query(ctx, `
WITH inserted AS (
    INSERT INTO channel_message_viewers (channel_id, message_id, viewer_user_id, viewed_at)
    SELECT m.channel_id, m.id, $3, $4
    FROM channel_messages m
    WHERE m.channel_id = $1
      AND m.id = ANY($2::int[])
      AND NOT m.deleted
      AND m.id > $5
      `+visibility+`
    ON CONFLICT DO NOTHING
    RETURNING message_id
), updated AS (
    UPDATE channel_messages m
    SET views_count = views_count + 1,
        updated_at = now()
    FROM inserted i
    WHERE m.channel_id = $1
      AND m.id = i.message_id
    RETURNING m.id
)
SELECT i.message_id
FROM inserted i
LEFT JOIN updated u ON u.id = i.message_id`, args...)
		if err != nil {
			return domain.ChannelMessageViewsResult{}, fmt.Errorf("increment channel message views: %w", err)
		}
		for rows.Next() {
			var ignored int
			if err := rows.Scan(&ignored); err != nil {
				rows.Close()
				return domain.ChannelMessageViewsResult{}, err
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return domain.ChannelMessageViewsResult{}, err
		}
		rows.Close()
	}
	summaries, err := s.listChannelMessageViewSummaries(ctx, req.UserID, channel, member, id32)
	if err != nil {
		return domain.ChannelMessageViewsResult{}, err
	}
	views := make(map[int]int, len(summaries))
	peers := make([]domain.Peer, 0, len(summaries))
	peerSeen := make(map[domain.Peer]struct{}, len(summaries))
	for _, summary := range summaries {
		views[summary.ID] = summary.Views
		if summary.Sender.ID != 0 {
			if _, ok := peerSeen[summary.Sender]; !ok {
				peerSeen[summary.Sender] = struct{}{}
				peers = append(peers, summary.Sender)
			}
		}
	}
	replies, err := s.channelMessageViewReplies(ctx, req.UserID, channel, summaries)
	if err != nil {
		return domain.ChannelMessageViewsResult{}, err
	}
	return domain.ChannelMessageViewsResult{
		Channel: channel,
		Views:   views,
		Replies: replies,
		Peers:   peers,
	}, nil
}

func (s *ChannelStore) listChannelMessageViewSummaries(ctx context.Context, viewerUserID int64, channel domain.Channel, member domain.ChannelMember, ids []int32) ([]channelMessageViewSummary, error) {
	args := []any{channel.ID, ids}
	where := "channel_id = $1 AND id = ANY($2::int[]) AND NOT deleted"
	if member.AvailableMinID > 0 {
		args = append(args, member.AvailableMinID)
		where += fmt.Sprintf(" AND id > $%d", len(args))
	}
	if channel.Monoforum && !member.CanManageDirectMessages() {
		args = append(args, string(domain.PeerTypeUser), viewerUserID)
		where += fmt.Sprintf(" AND saved_peer_type = $%d AND saved_peer_id = $%d", len(args)-1, len(args))
	}
	rows, err := s.db.Query(ctx, `
SELECT id, views_count, post, discussion_channel_id, discussion_message_id, sender_user_id, from_peer_type, from_peer_id
FROM channel_messages
WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("get channel message view summaries: %w", err)
	}
	defer rows.Close()
	out := make([]channelMessageViewSummary, 0, len(ids))
	for rows.Next() {
		var summary channelMessageViewSummary
		var discussionChannelID int64
		var discussionMessageID int
		var senderUserID int64
		var fromPeerType string
		var fromPeerID int64
		if err := rows.Scan(&summary.ID, &summary.Views, &summary.Post, &discussionChannelID, &discussionMessageID, &senderUserID, &fromPeerType, &fromPeerID); err != nil {
			return nil, err
		}
		if discussionChannelID != 0 && discussionMessageID != 0 {
			summary.Discussion = &domain.ChannelDiscussionRef{ChannelID: discussionChannelID, MessageID: discussionMessageID}
		}
		summary.Sender = domain.Peer{Type: domain.PeerType(fromPeerType), ID: fromPeerID}
		if summary.Sender.ID == 0 && senderUserID != 0 {
			summary.Sender = domain.Peer{Type: domain.PeerTypeUser, ID: senderUserID}
		}
		out = append(out, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *ChannelStore) channelMessageViewReplies(ctx context.Context, viewerUserID int64, channel domain.Channel, summaries []channelMessageViewSummary) (map[int]*domain.ChannelMessageReplies, error) {
	if len(summaries) == 0 || channel.ID == 0 {
		return map[int]*domain.ChannelMessageReplies{}, nil
	}
	indexes := make(map[channelReplyStatKey][]channelMessageViewReplyRef)
	channelIDs := make([]int64, 0, 2)
	rootIDs := make([]int32, 0, len(summaries))
	addRoot := func(messageID int, targetChannelID int64, rootID int, replies *domain.ChannelMessageReplies) {
		if targetChannelID == 0 || rootID <= 0 {
			return
		}
		key := channelReplyStatKey{channelID: targetChannelID, rootID: rootID}
		if _, ok := indexes[key]; !ok {
			channelIDs = append(channelIDs, targetChannelID)
			rootIDs = append(rootIDs, int32(rootID))
		}
		indexes[key] = append(indexes[key], channelMessageViewReplyRef{messageID: messageID, replies: replies})
	}
	out := make(map[int]*domain.ChannelMessageReplies)
	for _, summary := range summaries {
		targetChannelID := channel.ID
		rootID := summary.ID
		replies := &domain.ChannelMessageReplies{}
		if summary.Discussion != nil && summary.Discussion.ChannelID != 0 && summary.Discussion.MessageID != 0 {
			targetChannelID = summary.Discussion.ChannelID
			rootID = summary.Discussion.MessageID
			replies.Comments = true
			replies.ChannelID = summary.Discussion.ChannelID
		} else if channel.Broadcast && channel.LinkedChatID != 0 && summary.Post {
			targetChannelID = channel.LinkedChatID
			replies.Comments = true
			replies.ChannelID = channel.LinkedChatID
		}
		if replies.Comments {
			out[summary.ID] = replies
		}
		addRoot(summary.ID, targetChannelID, rootID, replies)
	}
	if len(channelIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.Query(ctx, `
WITH roots AS (
    SELECT *
    FROM unnest($1::bigint[], $2::int[]) AS r(channel_id, root_id)
)
SELECT r.channel_id,
       r.root_id,
       COUNT(m.id)::int,
       COALESCE(MAX(m.id), 0)::int,
       COALESCE((array_agg(m.pts ORDER BY m.id DESC) FILTER (WHERE m.id IS NOT NULL))[1], 0)::int,
       COALESCE(cm.read_inbox_max_id, 0)::int
FROM roots r
LEFT JOIN channel_members cm
  ON cm.channel_id = r.channel_id
 AND cm.user_id = $3
LEFT JOIN channel_messages m
  ON m.channel_id = r.channel_id
 AND m.reply_to_top_id = r.root_id
 AND NOT m.deleted
GROUP BY r.channel_id, r.root_id, cm.read_inbox_max_id`, channelIDs, rootIDs, viewerUserID)
	if err != nil {
		return nil, fmt.Errorf("load channel message view reply stats: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var channelID int64
		var rootID, count, maxID, repliesPts, readMaxID int
		if err := rows.Scan(&channelID, &rootID, &count, &maxID, &repliesPts, &readMaxID); err != nil {
			return nil, err
		}
		for _, ref := range indexes[channelReplyStatKey{channelID: channelID, rootID: rootID}] {
			replies := ref.replies
			replies.ReadMaxID = readMaxID
			if count > 0 {
				replies.Replies = count
				replies.MaxID = maxID
				replies.RepliesPts = repliesPts
				out[ref.messageID] = replies
			} else if replies.Comments {
				out[ref.messageID] = replies
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
