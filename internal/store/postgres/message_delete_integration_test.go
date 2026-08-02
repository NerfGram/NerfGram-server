package postgres

import (
	"context"
	"telesrv/internal/domain"
	"testing"
)

func TestMessageStoreDeleteMessagesRecomputesRecipientUnreadAndTop(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := randomSuffix(t)

	users := NewUserStore(pool)
	sender, err := users.Create(ctx, domain.User{
		AccessHash: 61,
		Phone:      "+1668" + suffix + "01",
		FirstName:  "DeleteSender",
	})
	if err != nil {
		t.Fatalf("create sender: %v", err)
	}
	recipient, err := users.Create(ctx, domain.User{
		AccessHash: 62,
		Phone:      "+1668" + suffix + "02",
		FirstName:  "DeleteRecipient",
	})
	if err != nil {
		t.Fatalf("create recipient: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = ANY($1::bigint[])", []int64{sender.ID, recipient.ID})
	})

	messages := NewMessageStore(pool)
	bodies := []string{"one", "two", "three"}
	sent := make([]domain.SendPrivateTextResult, 0, len(bodies))
	for i, body := range bodies {
		got, err := messages.SendPrivateText(ctx, domain.SendPrivateTextRequest{
			SenderUserID:    sender.ID,
			RecipientUserID: recipient.ID,
			RandomID:        824000 + int64(i),
			Message:         body,
			Date:            1700000500 + i,
		})
		if err != nil {
			t.Fatalf("SendPrivateText %q: %v", body, err)
		}
		sent = append(sent, got)
	}

	deleted, err := messages.DeleteMessages(ctx, domain.DeleteMessagesRequest{
		OwnerUserID: sender.ID,
		IDs:         []int{sent[2].SenderMessage.ID},
		Revoke:      true,
		Date:        1700000510,
	})
	if err != nil {
		t.Fatalf("DeleteMessages revoke latest: %v", err)
	}
	if len(deleted.Deleted) != 2 || !deleted.Changed() {
		t.Fatalf("deleted = %+v, want both owner boxes revoked", deleted)
	}

	var unreadCount, actualUnread, topMessageID int
	var topBody string
	if err := pool.QueryRow(ctx, `
		WITH target AS (
			SELECT d.unread_count, d.top_message_id, d.read_inbox_max_id, COALESCE(m.body, '') AS top_body
			FROM dialogs d
			LEFT JOIN message_boxes m
			  ON m.owner_user_id = d.user_id
			 AND m.box_id = d.top_message_id
			 AND NOT m.deleted
			WHERE d.user_id = $1
			  AND d.peer_type = $2
			  AND d.peer_id = $3
		),
		actual AS (
			SELECT COUNT(*)::int AS unread
			FROM message_boxes
			WHERE owner_user_id = $1
			  AND peer_type = $2
			  AND peer_id = $3
			  AND NOT deleted
			  AND NOT outgoing
			  AND box_id > (SELECT read_inbox_max_id FROM target)
		)
		SELECT target.unread_count, target.top_message_id, target.top_body, actual.unread
		FROM target CROSS JOIN actual
	`, recipient.ID, string(domain.PeerTypeUser), sender.ID).Scan(&unreadCount, &topMessageID, &topBody, &actualUnread); err != nil {
		t.Fatalf("load recipient dialog after revoke: %v", err)
	}
	if unreadCount != 2 || actualUnread != 2 || topMessageID != sent[1].RecipientMessage.ID || topBody != "two" {
		t.Fatalf("recipient dialog after revoke = unread %d actual %d top %d body %q, want unread=2 top=%d body=two",
			unreadCount, actualUnread, topMessageID, topBody, sent[1].RecipientMessage.ID)
	}
	events, err := NewUpdateEventStore(pool).ListAfter(ctx, recipient.ID, 3, 10)
	if err != nil {
		t.Fatalf("list recipient events after revoke: %v", err)
	}
	if len(events) != 1 ||
		events[0].Type != domain.UpdateEventDeleteMessages ||
		events[0].Pts != 4 ||
		events[0].PtsCount != 1 ||
		!sameInts(events[0].MessageIDs, []int{sent[2].RecipientMessage.ID}) {
		t.Fatalf("recipient events after revoke = %+v, want delete only; later unread messages are still live", events)
	}
}

func TestMessageStoreDeleteMiddleUnreadKeepsRecipientTop(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := randomSuffix(t)

	users := NewUserStore(pool)
	sender, err := users.Create(ctx, domain.User{
		AccessHash: 63,
		Phone:      "+1669" + suffix + "01",
		FirstName:  "MiddleDeleteSender",
	})
	if err != nil {
		t.Fatalf("create sender: %v", err)
	}
	recipient, err := users.Create(ctx, domain.User{
		AccessHash: 64,
		Phone:      "+1669" + suffix + "02",
		FirstName:  "MiddleDeleteRecipient",
	})
	if err != nil {
		t.Fatalf("create recipient: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = ANY($1::bigint[])", []int64{sender.ID, recipient.ID})
	})

	messages := NewMessageStore(pool)
	recipientPeer := domain.Peer{Type: domain.PeerTypeUser, ID: sender.ID}
	first, err := messages.SendPrivateText(ctx, domain.SendPrivateTextRequest{
		SenderUserID:    sender.ID,
		RecipientUserID: recipient.ID,
		RandomID:        824999,
		Message:         "already read",
		Date:            1700000590,
	})
	if err != nil {
		t.Fatalf("SendPrivateText first: %v", err)
	}
	if _, err := messages.ReadHistory(ctx, domain.ReadHistoryRequest{
		OwnerUserID: recipient.ID,
		Peer:        recipientPeer,
		MaxID:       first.RecipientMessage.ID,
		Date:        1700000595,
	}); err != nil {
		t.Fatalf("ReadHistory first: %v", err)
	}

	bodies := []string{"one", "two", "three"}
	sent := make([]domain.SendPrivateTextResult, 0, len(bodies))
	for i, body := range bodies {
		got, err := messages.SendPrivateText(ctx, domain.SendPrivateTextRequest{
			SenderUserID:    sender.ID,
			RecipientUserID: recipient.ID,
			RandomID:        825000 + int64(i),
			Message:         body,
			Date:            1700000600 + i,
		})
		if err != nil {
			t.Fatalf("SendPrivateText %q: %v", body, err)
		}
		sent = append(sent, got)
	}

	if _, err := messages.DeleteMessages(ctx, domain.DeleteMessagesRequest{
		OwnerUserID: sender.ID,
		IDs:         []int{sent[1].SenderMessage.ID},
		Revoke:      true,
		Date:        1700000610,
	}); err != nil {
		t.Fatalf("DeleteMessages revoke middle: %v", err)
	}

	var unreadCount, actualUnread, topMessageID, readInboxMaxID int
	var topBody string
	if err := pool.QueryRow(ctx, `
		WITH target AS (
			SELECT d.unread_count, d.top_message_id, d.read_inbox_max_id, COALESCE(m.body, '') AS top_body
			FROM dialogs d
			LEFT JOIN message_boxes m
			  ON m.owner_user_id = d.user_id
			 AND m.box_id = d.top_message_id
			 AND NOT m.deleted
			WHERE d.user_id = $1
			  AND d.peer_type = $2
			  AND d.peer_id = $3
		),
		actual AS (
			SELECT COUNT(*)::int AS unread
			FROM message_boxes
			WHERE owner_user_id = $1
			  AND peer_type = $2
			  AND peer_id = $3
			  AND NOT deleted
			  AND NOT outgoing
			  AND box_id > (SELECT read_inbox_max_id FROM target)
		)
		SELECT target.unread_count, target.top_message_id, target.read_inbox_max_id, target.top_body, actual.unread
		FROM target CROSS JOIN actual
	`, recipient.ID, string(domain.PeerTypeUser), sender.ID).Scan(&unreadCount, &topMessageID, &readInboxMaxID, &topBody, &actualUnread); err != nil {
		t.Fatalf("load recipient dialog after middle revoke: %v", err)
	}
	if unreadCount != 2 || actualUnread != 2 || topMessageID != sent[2].RecipientMessage.ID || readInboxMaxID != first.RecipientMessage.ID || topBody != "three" {
		t.Fatalf("recipient dialog after middle revoke = unread %d actual %d top %d read %d body %q, want unread=2 top=%d read=%d body=three",
			unreadCount, actualUnread, topMessageID, readInboxMaxID, topBody, sent[2].RecipientMessage.ID, first.RecipientMessage.ID)
	}
	events, err := NewUpdateEventStore(pool).ListAfter(ctx, recipient.ID, 5, 10)
	if err != nil {
		t.Fatalf("list recipient events after middle revoke: %v", err)
	}
	if len(events) != 1 ||
		events[0].Type != domain.UpdateEventDeleteMessages ||
		events[0].Pts != 6 ||
		events[0].PtsCount != 1 ||
		!sameInts(events[0].MessageIDs, []int{sent[1].RecipientMessage.ID}) {
		t.Fatalf("recipient events after middle revoke = %+v, want delete only; first unread is still live", events)
	}
}

func TestMessageStoreDeleteFirstUnreadEmitsSafeUnreadCorrection(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := randomSuffix(t)

	users := NewUserStore(pool)
	sender, err := users.Create(ctx, domain.User{
		AccessHash: 65,
		Phone:      "+1670" + suffix + "01",
		FirstName:  "FirstUnreadDeleteSender",
	})
	if err != nil {
		t.Fatalf("create sender: %v", err)
	}
	recipient, err := users.Create(ctx, domain.User{
		AccessHash: 66,
		Phone:      "+1670" + suffix + "02",
		FirstName:  "FirstUnreadDeleteRecipient",
	})
	if err != nil {
		t.Fatalf("create recipient: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = ANY($1::bigint[])", []int64{sender.ID, recipient.ID})
	})

	messages := NewMessageStore(pool)
	recipientPeer := domain.Peer{Type: domain.PeerTypeUser, ID: sender.ID}
	first, err := messages.SendPrivateText(ctx, domain.SendPrivateTextRequest{
		SenderUserID:    sender.ID,
		RecipientUserID: recipient.ID,
		RandomID:        826000,
		Message:         "already read",
		Date:            1700000640,
	})
	if err != nil {
		t.Fatalf("SendPrivateText first: %v", err)
	}
	if _, err := messages.ReadHistory(ctx, domain.ReadHistoryRequest{
		OwnerUserID: recipient.ID,
		Peer:        recipientPeer,
		MaxID:       first.RecipientMessage.ID,
		Date:        1700000645,
	}); err != nil {
		t.Fatalf("ReadHistory first: %v", err)
	}

	bodies := []string{"one", "two", "three"}
	sent := make([]domain.SendPrivateTextResult, 0, len(bodies))
	for i, body := range bodies {
		got, err := messages.SendPrivateText(ctx, domain.SendPrivateTextRequest{
			SenderUserID:    sender.ID,
			RecipientUserID: recipient.ID,
			RandomID:        826001 + int64(i),
			Message:         body,
			Date:            1700000650 + i,
		})
		if err != nil {
			t.Fatalf("SendPrivateText %q: %v", body, err)
		}
		sent = append(sent, got)
	}

	if _, err := messages.DeleteMessages(ctx, domain.DeleteMessagesRequest{
		OwnerUserID: sender.ID,
		IDs:         []int{sent[0].SenderMessage.ID},
		Revoke:      true,
		Date:        1700000660,
	}); err != nil {
		t.Fatalf("DeleteMessages revoke first unread: %v", err)
	}

	events, err := NewUpdateEventStore(pool).ListAfter(ctx, recipient.ID, 5, 10)
	if err != nil {
		t.Fatalf("list recipient events after first unread revoke: %v", err)
	}
	if len(events) != 2 ||
		events[0].Type != domain.UpdateEventDeleteMessages ||
		events[0].Pts != 6 ||
		events[0].PtsCount != 1 ||
		!sameInts(events[0].MessageIDs, []int{sent[0].RecipientMessage.ID}) ||
		events[1].Type != domain.UpdateEventReadHistoryInbox ||
		events[1].Pts != 7 ||
		events[1].PtsCount != 1 ||
		events[1].MaxID != sent[0].RecipientMessage.ID ||
		events[1].StillUnreadCount != 2 {
		t.Fatalf("recipient events after first unread revoke = %+v, want delete then prefix unread correction", events)
	}
}

func TestMessageStoreDeleteHistoryRebuildsDialogAndEmitsDeleteUpdates(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := randomSuffix(t)

	users := NewUserStore(pool)
	sender := createTestUser(t, ctx, users, "+1991"+suffix+"01", "DeleteSender", "")
	recipient := createTestUser(t, ctx, users, "+1991"+suffix+"02", "DeleteRecipient", "")
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = ANY($1::bigint[])", []int64{sender.ID, recipient.ID})
	})

	messages := NewMessageStore(pool)
	for i := 0; i < 2; i++ {
		if _, err := messages.SendPrivateText(ctx, domain.SendPrivateTextRequest{
			SenderUserID:    sender.ID,
			RecipientUserID: recipient.ID,
			RandomID:        int64(7000 + i),
			Message:         "history",
			Date:            1700000700 + i,
		}); err != nil {
			t.Fatalf("seed send %d: %v", i, err)
		}
	}
	peer := domain.Peer{Type: domain.PeerTypeUser, ID: recipient.ID}
	deleted, err := messages.DeleteHistory(ctx, domain.DeleteHistoryRequest{
		OwnerUserID: sender.ID,
		Peer:        peer,
		Date:        1700000800,
	})
	if err != nil {
		t.Fatalf("DeleteHistory: %v", err)
	}
	if self := deleted.Self(); self.Event.Pts != 4 || self.Event.PtsCount != 2 || len(self.MessageIDs) != 2 {
		t.Fatalf("delete result = %+v, want sender delete range pts=4 count=2 ids", self)
	}
	senderHistory, err := messages.ListByUser(ctx, sender.ID, domain.MessageFilter{HasPeer: true, Peer: peer, Limit: 10})
	if err != nil {
		t.Fatalf("sender history: %v", err)
	}
	recipientHistory, err := messages.ListByUser(ctx, recipient.ID, domain.MessageFilter{HasPeer: true, Peer: domain.Peer{Type: domain.PeerTypeUser, ID: sender.ID}, Limit: 10})
	if err != nil {
		t.Fatalf("recipient history: %v", err)
	}
	if len(senderHistory.Messages) != 0 || len(recipientHistory.Messages) != 2 {
		t.Fatalf("history sizes sender=%d recipient=%d, want sender cleared only", len(senderHistory.Messages), len(recipientHistory.Messages))
	}
	senderDialogs, err := NewDialogStore(pool).ListByUser(ctx, sender.ID, domain.DialogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("sender dialogs after delete: %v", err)
	}
	if len(senderDialogs.Dialogs) != 0 {
		t.Fatalf("sender dialogs = %+v, want empty after full history delete", senderDialogs.Dialogs)
	}
	events, err := NewUpdateEventStore(pool).ListAfter(ctx, sender.ID, 2, 10)
	if err != nil {
		t.Fatalf("list sender events: %v", err)
	}
	if len(events) != 1 || events[0].Type != domain.UpdateEventDeleteMessages || events[0].Pts != 4 || events[0].PtsCount != 2 || len(events[0].MessageIDs) != 2 {
		t.Fatalf("events = %+v, want delete messages event pts=4 pts_count=2", events)
	}

	rebuilt, err := messages.SendPrivateText(ctx, domain.SendPrivateTextRequest{
		SenderUserID:    sender.ID,
		RecipientUserID: recipient.ID,
		RandomID:        8000,
		Message:         "after clear",
		Date:            1700000900,
	})
	if err != nil {
		t.Fatalf("send after delete: %v", err)
	}
	senderDialogs, err = NewDialogStore(pool).ListByUser(ctx, sender.ID, domain.DialogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("sender dialogs after rebuild: %v", err)
	}
	if len(senderDialogs.Dialogs) != 1 || senderDialogs.Dialogs[0].Peer != peer || senderDialogs.Dialogs[0].TopMessage != rebuilt.SenderMessage.ID {
		t.Fatalf("rebuilt dialogs = %+v, want new top message %d", senderDialogs.Dialogs, rebuilt.SenderMessage.ID)
	}

	revoked, err := messages.DeleteMessages(ctx, domain.DeleteMessagesRequest{
		OwnerUserID: sender.ID,
		IDs:         []int{rebuilt.SenderMessage.ID},
		Revoke:      true,
		Date:        1700001000,
	})
	if err != nil {
		t.Fatalf("DeleteMessages revoke: %v", err)
	}
	if len(revoked.Deleted) != 2 || !revoked.Changed() {
		t.Fatalf("revoked = %+v, want delete events for both owners", revoked)
	}
	senderHistory, err = messages.ListByUser(ctx, sender.ID, domain.MessageFilter{HasPeer: true, Peer: peer, Limit: 10})
	if err != nil {
		t.Fatalf("sender history after revoke: %v", err)
	}
	recipientHistory, err = messages.ListByUser(ctx, recipient.ID, domain.MessageFilter{HasPeer: true, Peer: domain.Peer{Type: domain.PeerTypeUser, ID: sender.ID}, Limit: 10})
	if err != nil {
		t.Fatalf("recipient history after revoke: %v", err)
	}
	if len(senderHistory.Messages) != 0 || len(recipientHistory.Messages) != 2 {
		t.Fatalf("history sizes after revoke sender=%d recipient=%d, want new message removed from both owners", len(senderHistory.Messages), len(recipientHistory.Messages))
	}
}

func TestMessageStoreDeleteHistoryJustClearPreservesHistoryClearMessage(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := randomSuffix(t)

	users := NewUserStore(pool)
	owner := createTestUser(t, ctx, users, "+1992"+suffix+"01", "ClearOwner", "")
	peerUser := createTestUser(t, ctx, users, "+1992"+suffix+"02", "ClearPeer", "")
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = ANY($1::bigint[])", []int64{owner.ID, peerUser.ID})
	})

	messages := NewMessageStore(pool)
	if _, err := messages.SendPrivateText(ctx, domain.SendPrivateTextRequest{
		SenderUserID:    owner.ID,
		RecipientUserID: peerUser.ID,
		RandomID:        9000,
		Message:         "clear but keep dialog",
		Date:            1700001100,
	}); err != nil {
		t.Fatalf("seed send: %v", err)
	}
	peer := domain.Peer{Type: domain.PeerTypeUser, ID: peerUser.ID}
	clearResult, err := messages.DeleteHistory(ctx, domain.DeleteHistoryRequest{
		OwnerUserID:     owner.ID,
		Peer:            peer,
		JustClear:       true,
		Date:            1700001200,
		OriginAuthKeyID: [8]byte{7},
		OriginSessionID: 99,
	})
	if err != nil {
		t.Fatalf("DeleteHistory just_clear: %v", err)
	}
	self := clearResult.Self()
	if self.PtsCount != 2 || len(self.MessageIDs) != 0 || len(self.Events) != 2 ||
		self.Events[0].Type != domain.UpdateEventReadHistoryInbox ||
		self.Events[1].Type != domain.UpdateEventEditMessage {
		t.Fatalf("clear result = %+v, want read+edit", self)
	}
	dialogs, err := NewDialogStore(pool).ListByUser(ctx, owner.ID, domain.DialogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("dialogs after just_clear: %v", err)
	}
	if len(dialogs.Dialogs) != 1 || dialogs.Dialogs[0].Peer != peer ||
		dialogs.Dialogs[0].TopMessage == 0 || len(dialogs.Messages) != 1 ||
		!domain.IsHistoryClearServiceMessage(dialogs.Messages[0]) {
		t.Fatalf("dialogs = %+v messages=%+v, want real history-clear top", dialogs.Dialogs, dialogs.Messages)
	}
	history, err := messages.ListByUser(ctx, owner.ID, domain.MessageFilter{HasPeer: true, Peer: peer, Limit: 10, NeedTotalCount: true})
	if err != nil {
		t.Fatalf("history after just_clear: %v", err)
	}
	if len(history.Messages) != 1 || history.Messages[0].ID != dialogs.Dialogs[0].TopMessage ||
		!domain.IsHistoryClearServiceMessage(history.Messages[0]) ||
		!history.Messages[0].Out || history.Messages[0].From.ID != owner.ID ||
		history.Messages[0].Body != "" || history.Messages[0].MediaUnread ||
		history.Messages[0].ReactionUnread || history.Messages[0].Pinned {
		t.Fatalf("history = %+v, want clean owner-local history-clear anchor", history.Messages)
	}
	events, err := NewUpdateEventStore(pool).ListAfter(ctx, owner.ID, self.Pts-2, 10)
	if err != nil {
		t.Fatalf("list clear events: %v", err)
	}
	if len(events) != 2 || events[0].Type != domain.UpdateEventReadHistoryInbox ||
		events[1].Type != domain.UpdateEventEditMessage ||
		!domain.IsHistoryClearServiceMessage(events[1].Message) {
		t.Fatalf("durable clear events = %+v, want read/edit with service message", events)
	}
	var outboxRows int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int
FROM dispatch_outbox
WHERE target_user_id = $1
  AND pts = ANY($2::int[])
  AND exclude_auth_key_id = $3
  AND exclude_session_id = $4`,
		owner.ID, []int32{int32(self.Pts - 1), int32(self.Pts)}, int64(7), int64(99)).Scan(&outboxRows); err != nil {
		t.Fatalf("count clear outbox: %v", err)
	}
	if outboxRows != 2 {
		t.Fatalf("clear outbox rows = %d, want read/edit excluding origin session", outboxRows)
	}
	repeated, err := messages.DeleteHistory(ctx, domain.DeleteHistoryRequest{
		OwnerUserID: owner.ID, Peer: peer, JustClear: true, Date: 1700001201,
	})
	if err != nil {
		t.Fatalf("repeat just_clear: %v", err)
	}
	if repeated.Changed() || len(repeated.Deleted) != 0 {
		t.Fatalf("repeat just_clear = %+v, want idempotent no-op", repeated)
	}
}

func TestMessageStoreDeleteHistoryJustClearRevokeKeepsPerOwnerAnchors(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := randomSuffix(t)
	users := NewUserStore(pool)
	alice := createTestUser(t, ctx, users, "+1994"+suffix+"01", "ClearAlice", "")
	bob := createTestUser(t, ctx, users, "+1994"+suffix+"02", "ClearBob", "")
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = ANY($1::bigint[])", []int64{alice.ID, bob.ID})
	})

	messages := NewMessageStore(pool)
	var last domain.SendPrivateTextResult
	for i := 0; i < 2; i++ {
		var err error
		last, err = messages.SendPrivateText(ctx, domain.SendPrivateTextRequest{
			SenderUserID: alice.ID, RecipientUserID: bob.ID, RandomID: int64(9200 + i),
			Message: "clear for both", Date: 1700001300 + i,
		})
		if err != nil {
			t.Fatalf("seed send %d: %v", i, err)
		}
	}
	res, err := messages.DeleteHistory(ctx, domain.DeleteHistoryRequest{
		OwnerUserID: alice.ID,
		Peer:        domain.Peer{Type: domain.PeerTypeUser, ID: bob.ID},
		JustClear:   true,
		Revoke:      true,
		Date:        1700001400,
	})
	if err != nil {
		t.Fatalf("revoke just_clear: %v", err)
	}
	if len(res.Deleted) != 2 {
		t.Fatalf("deleted owners = %+v, want alice and bob", res.Deleted)
	}
	for _, tc := range []struct {
		userID int64
		peerID int64
		topID  int
	}{
		{alice.ID, bob.ID, last.SenderMessage.ID},
		{bob.ID, alice.ID, last.RecipientMessage.ID},
	} {
		history, err := messages.ListByUser(ctx, tc.userID, domain.MessageFilter{
			HasPeer: true, Peer: domain.Peer{Type: domain.PeerTypeUser, ID: tc.peerID}, Limit: 10,
		})
		if err != nil {
			t.Fatalf("history user %d: %v", tc.userID, err)
		}
		if len(history.Messages) != 1 || history.Messages[0].ID != tc.topID ||
			!domain.IsHistoryClearServiceMessage(history.Messages[0]) ||
			!history.Messages[0].Out || history.Messages[0].From.ID != tc.userID {
			t.Fatalf("history user %d = %+v, want owner-local anchor %d", tc.userID, history.Messages, tc.topID)
		}
		var ownerResult domain.DeletedMessagesForUser
		for _, item := range res.Deleted {
			if item.UserID == tc.userID {
				ownerResult = item
				break
			}
		}
		if ownerResult.PtsCount != 3 || len(ownerResult.MessageIDs) != 1 ||
			len(ownerResult.Events) != 3 {
			t.Fatalf("owner %d result = %+v, want delete/read/edit", tc.userID, ownerResult)
		}
	}
}

func TestMessageStoreDeleteHistoryJustClearKeepsAnchorAcrossBatches(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := randomSuffix(t)

	users := NewUserStore(pool)
	owner := createTestUser(t, ctx, users, "+1993"+suffix+"01", "BulkOwner", "")
	peerUser := createTestUser(t, ctx, users, "+1993"+suffix+"02", "BulkPeer", "")
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = ANY($1::bigint[])", []int64{owner.ID, peerUser.ID})
	})

	total := domain.MaxDeleteHistoryBatch + 2
	if _, err := pool.Exec(ctx, `
		WITH src AS (
		  SELECT generate_series(1, $3::int) AS g
		),
		pm AS (
		  INSERT INTO private_messages (
		    sender_user_id,
		    recipient_user_id,
		    random_id,
		    request_fingerprint,
		    recipient_delivered,
		    sender_box_id,
		    sender_pts,
		    recipient_box_id,
		    recipient_pts,
		    message_date,
		    body,
		    entities
		  )
		  SELECT
		    $1::bigint,
		    $2::bigint,
		    910000000 + g,
		    decode(repeat('00', 32), 'hex'),
		    false,
		    g,
		    g,
		    0,
		    0,
		    1700002000 + g,
		    'bulk history',
		    '[]'::jsonb
		  FROM src
		  RETURNING id, random_id, message_date
		)
		INSERT INTO message_boxes (
		  owner_user_id,
		  box_id,
		  private_message_id,
		  message_sender_id,
		  peer_type,
		  peer_id,
		  from_user_id,
		  message_date,
		  outgoing,
		  body,
		  entities,
		  pts
		)
		SELECT
		  $1::bigint,
		  (random_id - 910000000)::int,
		  id,
		  $1::bigint,
		  'user',
		  $2::bigint,
		  $1::bigint,
		  message_date,
		  true,
		  'bulk history',
		  '[]'::jsonb,
		  (random_id - 910000000)::int
		FROM pm
	`, owner.ID, peerUser.ID, total); err != nil {
		t.Fatalf("seed bulk history: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO dialogs (
		  user_id,
		  peer_type,
		  peer_id,
		  top_message_id,
		  top_message_date,
		  read_outbox_max_id,
		  unread_count
		) VALUES ($1, 'user', $2, $3, $4, $3, 0)
	`, owner.ID, peerUser.ID, total, 1700002000+total); err != nil {
		t.Fatalf("seed dialog: %v", err)
	}

	messages := NewMessageStore(pool)
	peer := domain.Peer{Type: domain.PeerTypeUser, ID: peerUser.ID}
	first, err := messages.DeleteHistory(ctx, domain.DeleteHistoryRequest{
		OwnerUserID: owner.ID,
		Peer:        peer,
		JustClear:   true,
		Date:        1700003000,
	})
	if err != nil {
		t.Fatalf("DeleteHistory first batch: %v", err)
	}
	self := first.Self()
	if first.Offset != 1 || self.Event.Pts != domain.MaxDeleteHistoryBatch ||
		self.Event.PtsCount != domain.MaxDeleteHistoryBatch ||
		self.Pts != domain.MaxDeleteHistoryBatch+2 || self.PtsCount != domain.MaxDeleteHistoryBatch+2 ||
		len(self.MessageIDs) != domain.MaxDeleteHistoryBatch || len(self.Events) != 3 {
		t.Fatalf("first batch = %+v self=%+v, want delete batch plus read/edit", first, self)
	}
	history, err := messages.ListByUser(ctx, owner.ID, domain.MessageFilter{HasPeer: true, Peer: peer, Limit: 10, NeedTotalCount: true})
	if err != nil {
		t.Fatalf("history after first batch: %v", err)
	}
	if history.Count != 2 || len(history.Messages) != 2 || history.Messages[0].ID != total ||
		!domain.IsHistoryClearServiceMessage(history.Messages[0]) || history.Messages[1].ID != 1 {
		t.Fatalf("history after first batch = %+v, want stable top anchor plus oldest message", history)
	}

	second, err := messages.DeleteHistory(ctx, domain.DeleteHistoryRequest{
		OwnerUserID: owner.ID,
		Peer:        peer,
		JustClear:   true,
		Date:        1700003001,
	})
	if err != nil {
		t.Fatalf("DeleteHistory second batch: %v", err)
	}
	if second.Offset != 0 || second.Self().Event.PtsCount != 1 ||
		second.Self().PtsCount != 1 || len(second.Self().Events) != 1 {
		t.Fatalf("second batch = %+v, want final old-message delete only", second)
	}
	history, err = messages.ListByUser(ctx, owner.ID, domain.MessageFilter{HasPeer: true, Peer: peer, Limit: 10, NeedTotalCount: true})
	if err != nil {
		t.Fatalf("history after second batch: %v", err)
	}
	if history.Count != 1 || len(history.Messages) != 1 || history.Messages[0].ID != total ||
		!domain.IsHistoryClearServiceMessage(history.Messages[0]) {
		t.Fatalf("history after second batch = %+v, want only stable history-clear anchor", history)
	}
}
