package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"telesrv/internal/domain"
)

func TestPostgresPrivateNoForwardsAtomicStateAndDifference(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := randomSuffix(t)
	users := NewUserStore(pool)
	alice, err := users.Create(ctx, domain.User{AccessHash: 6101, Phone: "+1668" + suffix + "01", FirstName: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := users.Create(ctx, domain.User{AccessHash: 6102, Phone: "+1668" + suffix + "02", FirstName: "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = ANY($1::bigint[])", []int64{alice.ID, bob.ID})
	})

	messages := NewMessageStore(pool)
	baseRandom := time.Now().UnixNano()
	enable, err := messages.TogglePrivateNoForwards(ctx, domain.TogglePrivateNoForwardsRequest{
		ActorUserID: alice.ID, PeerUserID: bob.ID, Enabled: true, RandomID: baseRandom, Date: 1700100000,
	})
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	request, err := messages.TogglePrivateNoForwards(ctx, domain.TogglePrivateNoForwardsRequest{
		ActorUserID: bob.ID, PeerUserID: alice.ID, RandomID: baseRandom + 1, Date: 1700100001,
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	answer, err := messages.TogglePrivateNoForwards(ctx, domain.TogglePrivateNoForwardsRequest{
		ActorUserID: alice.ID, PeerUserID: bob.ID, RequestMsgID: request.Send.RecipientMessage.ID,
		RandomID: baseRandom + 2, Date: 1700100002,
	})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if enable.Send.SenderMessage.Pts != 1 || request.Send.SenderMessage.Pts != 2 ||
		answer.Send.SenderMessage.Pts != 3 || answer.Send.SenderMessage.ReplyTo == nil ||
		answer.Send.SenderMessage.ReplyTo.MessageID != request.Send.RecipientMessage.ID ||
		answer.Send.RecipientMessage.ReplyTo == nil ||
		answer.Send.RecipientMessage.ReplyTo.MessageID != request.Send.SenderMessage.ID {
		t.Fatalf("pts/reply mapping enable=%+v request=%+v answer=%+v", enable.Send, request.Send, answer.Send)
	}
	state, err := messages.GetPrivateNoForwards(ctx, alice.ID, bob.ID)
	if err != nil || state.Enabled() {
		t.Fatalf("final state=%+v err=%v, want disabled", state, err)
	}
	if _, err := messages.TogglePrivateNoForwards(ctx, domain.TogglePrivateNoForwardsRequest{
		ActorUserID: alice.ID, PeerUserID: bob.ID, RequestMsgID: request.Send.RecipientMessage.ID,
		RandomID: baseRandom + 3, Date: 1700100003,
	}); !errors.Is(err, domain.ErrNoForwardsRequestExpired) {
		t.Fatalf("repeat answer err=%v", err)
	}

	for _, userID := range []int64{alice.ID, bob.ID} {
		events, err := NewUpdateEventStore(pool).ListAfter(ctx, userID, 0, 10)
		if err != nil {
			t.Fatalf("events user %d: %v", userID, err)
		}
		if len(events) != 3 || events[0].Pts != 1 || events[1].Pts != 2 || events[2].Pts != 3 {
			t.Fatalf("events user %d = %+v, want continuous 1..3", userID, events)
		}
	}
	var eventCount, outboxCount int
	if err := pool.QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM user_update_events WHERE user_id = ANY($1::bigint[])),
  (SELECT count(*) FROM dispatch_outbox WHERE target_user_id = ANY($1::bigint[]))`,
		[]int64{alice.ID, bob.ID},
	).Scan(&eventCount, &outboxCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 6 || outboxCount != 6 {
		t.Fatalf("event/outbox count=%d/%d, want 6/6", eventCount, outboxCount)
	}
	var handledAt int
	var logicalExpired bool
	var expiredBoxes int
	if err := pool.QueryRow(ctx, `
SELECT r.handled_at,
       COALESCE((pm.media #>> '{service_action,no_forwards,expired}')::boolean, false),
       (SELECT count(*)
        FROM message_boxes b
        WHERE b.message_sender_id = r.private_message_sender_user_id
          AND b.private_message_id = r.private_message_id
          AND COALESCE((b.media #>> '{service_action,no_forwards,expired}')::boolean, false))
FROM private_no_forwards_requests r
JOIN private_messages pm
  ON pm.sender_user_id = r.private_message_sender_user_id
 AND pm.id = r.private_message_id
WHERE r.private_message_sender_user_id = $1
  AND r.private_message_id = $2`, bob.ID, request.Send.SenderMessage.UID,
	).Scan(&handledAt, &logicalExpired, &expiredBoxes); err != nil {
		t.Fatal(err)
	}
	if handledAt != 1700100002 || !logicalExpired || expiredBoxes != 2 {
		t.Fatalf("handled request handled_at=%d logical_expired=%v boxes=%d", handledAt, logicalExpired, expiredBoxes)
	}
}

func TestPostgresPrivateNoForwardsConcurrentOwnershipAndOneShotAnswer(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := randomSuffix(t)
	users := NewUserStore(pool)
	alice, err := users.Create(ctx, domain.User{AccessHash: 6201, Phone: "+1669" + suffix + "01", FirstName: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := users.Create(ctx, domain.User{AccessHash: 6202, Phone: "+1669" + suffix + "02", FirstName: "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = ANY($1::bigint[])", []int64{alice.ID, bob.ID})
	})

	messages := NewMessageStore(pool)
	baseRandom := time.Now().UnixNano()
	enableResults := make([]domain.TogglePrivateNoForwardsResult, 2)
	enableErrors := make([]error, 2)
	actors := []int64{alice.ID, bob.ID}
	peers := []int64{bob.ID, alice.ID}
	var wg sync.WaitGroup
	for i := range actors {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			enableResults[i], enableErrors[i] = messages.TogglePrivateNoForwards(ctx, domain.TogglePrivateNoForwardsRequest{
				ActorUserID: actors[i],
				PeerUserID:  peers[i],
				Enabled:     true,
				RandomID:    baseRandom + int64(i),
				Date:        1700200000,
			})
		}(i)
	}
	wg.Wait()
	changed := 0
	for i, err := range enableErrors {
		if err != nil {
			t.Fatalf("concurrent enable %d: %v", i, err)
		}
		if enableResults[i].Changed {
			changed++
		}
	}
	if changed != 1 {
		t.Fatalf("concurrent enable changed=%d, want exactly one service message", changed)
	}
	state, err := messages.GetPrivateNoForwards(ctx, alice.ID, bob.ID)
	if err != nil || (state.EnabledByUserID != alice.ID && state.EnabledByUserID != bob.ID) {
		t.Fatalf("concurrent enable state=%+v err=%v", state, err)
	}

	ownerID := state.EnabledByUserID
	requesterID := alice.ID
	if ownerID == alice.ID {
		requesterID = bob.ID
	}
	request, err := messages.TogglePrivateNoForwards(ctx, domain.TogglePrivateNoForwardsRequest{
		ActorUserID: requesterID,
		PeerUserID:  ownerID,
		RandomID:    baseRandom + 10,
		Date:        1700200001,
	})
	if err != nil {
		t.Fatalf("create disable request: %v", err)
	}

	answerResults := make([]domain.TogglePrivateNoForwardsResult, 2)
	answerErrors := make([]error, 2)
	for i := range answerResults {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			answerResults[i], answerErrors[i] = messages.TogglePrivateNoForwards(ctx, domain.TogglePrivateNoForwardsRequest{
				ActorUserID:  ownerID,
				PeerUserID:   requesterID,
				Enabled:      false,
				RequestMsgID: request.Send.RecipientMessage.ID,
				RandomID:     baseRandom + 20 + int64(i),
				Date:         1700200002,
			})
		}(i)
	}
	wg.Wait()
	successes, expired := 0, 0
	for i, err := range answerErrors {
		switch {
		case err == nil && answerResults[i].Changed:
			successes++
		case errors.Is(err, domain.ErrNoForwardsRequestExpired):
			expired++
		default:
			t.Fatalf("concurrent answer %d result=%+v err=%v", i, answerResults[i], err)
		}
	}
	if successes != 1 || expired != 1 {
		t.Fatalf("concurrent answers successes=%d expired=%d, want 1/1", successes, expired)
	}
	state, err = messages.GetPrivateNoForwards(ctx, alice.ID, bob.ID)
	if err != nil || state.Enabled() {
		t.Fatalf("state after concurrent answer=%+v err=%v, want disabled", state, err)
	}
	for _, userID := range []int64{alice.ID, bob.ID} {
		events, err := NewUpdateEventStore(pool).ListAfter(ctx, userID, 0, 10)
		if err != nil {
			t.Fatalf("events user %d: %v", userID, err)
		}
		if len(events) != 3 || events[0].Pts != 1 || events[1].Pts != 2 || events[2].Pts != 3 {
			t.Fatalf("events user %d = %+v, want one enable/request/answer sequence", userID, events)
		}
	}
}
