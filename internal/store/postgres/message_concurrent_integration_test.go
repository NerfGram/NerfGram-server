package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	"telesrv/internal/domain"
)

// TestSendPrivateTextConcurrentNoPtsGap 是「PG 事务内分配 pts」的正确性核心证明：
// N 条消息并发 sender→recipient 发送，全部成功提交后，接收方账号事件 pts 必须严格连续 1..N
// （无空洞、无重复、无丢失），且 MaxContiguousPts == N。
// box id 仍可由外部计数器提供，pts 则由 user_update_watermarks 在同一 PG tx 内推进。
func TestSendPrivateTextConcurrentNoPtsGap(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := randomSuffix(t)

	users := NewUserStore(pool)
	sender, err := users.Create(ctx, domain.User{
		AccessHash: 51,
		Phone:      "+1999" + suffix + "01",
		FirstName:  "ConcSender",
	})
	if err != nil {
		t.Fatalf("create sender: %v", err)
	}
	recipient, err := users.Create(ctx, domain.User{
		AccessHash: 52,
		Phone:      "+1999" + suffix + "02",
		FirstName:  "ConcRecipient",
	})
	if err != nil {
		t.Fatalf("create recipient: %v", err)
	}
	ids := []int64{sender.ID, recipient.ID}
	t.Cleanup(func() {
		// 按 FK 依赖序清理子表（message_boxes.from_user_id 为 RESTRICT）再删用户。
		_, _ = pool.Exec(ctx, "DELETE FROM dispatch_outbox WHERE target_user_id = ANY($1::bigint[])", ids)
		_, _ = pool.Exec(ctx, "DELETE FROM user_update_events WHERE user_id = ANY($1::bigint[])", ids)
		_, _ = pool.Exec(ctx, "DELETE FROM message_boxes WHERE owner_user_id = ANY($1::bigint[])", ids)
		_, _ = pool.Exec(ctx, "DELETE FROM private_messages WHERE sender_user_id = ANY($1::bigint[])", ids)
		_, _ = pool.Exec(ctx, "DELETE FROM dialogs WHERE user_id = ANY($1::bigint[])", ids)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = ANY($1::bigint[])", ids)
	})
	var initialWatermarks int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int
FROM user_update_watermarks
WHERE user_id = ANY($1::bigint[])`, ids).Scan(&initialWatermarks); err != nil {
		t.Fatalf("count initial watermarks: %v", err)
	}
	if initialWatermarks != 0 {
		t.Fatalf("initial watermarks = %d, want 0 so concurrency covers first upsert", initialWatermarks)
	}

	messages := NewMessageStore(pool, WithMessageAllocators(&perUserCounterAllocator{}))

	const n = 200
	base := time.Now().UnixNano()
	date := int(time.Now().Unix())
	errs := make([]error, n)
	recipPts := make([]int, n)
	sem := make(chan struct{}, 32)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			res, err := messages.SendPrivateText(ctx, domain.SendPrivateTextRequest{
				SenderUserID:    sender.ID,
				RecipientUserID: recipient.ID,
				RandomID:        base + int64(i),
				Message:         "concurrent body",
				Date:            date,
			})
			errs[i] = err
			recipPts[i] = res.RecipientMessage.Pts
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}

	// 进程内检测 allocator 是否给接收方分配了重复 pts。
	seen := map[int]int{}
	for i := 0; i < n; i++ {
		if prev, ok := seen[recipPts[i]]; ok {
			t.Errorf("recipient pts %d 被发送 #%d 和 #%d 同时分配（allocator 并发重复）", recipPts[i], prev, i)
		}
		seen[recipPts[i]] = i
	}

	// 接收方应有 n 条事件，pts 连续 1..n（无空洞无重复无丢失）。
	events := NewUpdateEventStore(pool)
	got, err := events.ListAfter(ctx, recipient.ID, 0, n+10)
	if err != nil {
		t.Fatalf("list recipient events: %v", err)
	}
	if len(got) != n {
		t.Fatalf("recipient events = %d, want %d（无丢失无重复）", len(got), n)
	}
	for i, ev := range got {
		if ev.Pts != i+1 {
			t.Fatalf("recipient event[%d].Pts = %d, want %d（pts 必须连续无洞）", i, ev.Pts, i+1)
		}
	}
	contig, err := events.MaxContiguousPts(ctx, recipient.ID)
	if err != nil {
		t.Fatalf("MaxContiguousPts: %v", err)
	}
	if contig != n {
		t.Fatalf("MaxContiguousPts(recipient) = %d, want %d", contig, n)
	}
}
