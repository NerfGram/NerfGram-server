package postgres

import (
	"context"
	"testing"
	"time"

	"telesrv/internal/domain"
)

func TestStarGiftLedgerTransactionDirectionsPostgres(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	ownerID := (time.Now().UnixNano() & 0x1fffffffffffffff) + 3_000_000_000
	channelID := ownerID + 1
	lifecycle := NewStarGiftLifecycleStore(pool, nil, 0)

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM ton_transactions WHERE user_id=$1`, ownerID)
		_, _ = pool.Exec(ctx, `DELETE FROM ton_balances WHERE user_id=$1`, ownerID)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_stars_transactions WHERE channel_id=$1`, channelID)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_stars_balances WHERE channel_id=$1`, channelID)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_ton_transactions WHERE channel_id=$1`, channelID)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_ton_balances WHERE channel_id=$1`, channelID)
	})

	if _, err := pool.Exec(ctx, `INSERT INTO ton_balances(user_id,balance_nanoton,granted) VALUES($1,70,true)`, ownerID); err != nil {
		t.Fatalf("insert ton balance: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channel_stars_balances(channel_id,balance) VALUES($1,70)`, channelID); err != nil {
		t.Fatalf("insert channel stars balance: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channel_ton_balances(channel_id,balance_nanoton) VALUES($1,70)`, channelID); err != nil {
		t.Fatalf("insert channel ton balance: %v", err)
	}
	for i, amount := range []int64{100, -40, 20, -10} {
		date := 1_800_000_000 + i
		if _, err := pool.Exec(ctx, `INSERT INTO ton_transactions(user_id,amount_nanoton,reason,date) VALUES($1,$2,'adjust',$3)`, ownerID, amount, date); err != nil {
			t.Fatalf("insert ton transaction %d: %v", i, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO channel_stars_transactions(channel_id,actor_user_id,amount,reason,date) VALUES($1,$2,$3,'adjust',$4)`, channelID, ownerID, amount, date); err != nil {
			t.Fatalf("insert channel stars transaction %d: %v", i, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO channel_ton_transactions(channel_id,actor_user_id,amount_nanoton,reason,date) VALUES($1,$2,$3,'adjust',$4)`, channelID, ownerID, amount, date); err != nil {
			t.Fatalf("insert channel ton transaction %d: %v", i, err)
		}
	}

	tonIncoming, err := lifecycle.TonTransactions(ctx, ownerID, domain.StarsTransactionQuery{
		Limit: 10, Direction: domain.StarsTransactionDirectionIncoming,
	})
	if err != nil {
		t.Fatalf("personal ton incoming: %v", err)
	}
	assertTonTransactionAmounts(t, tonIncoming.Transactions, []int64{20, 100})

	tonOutgoing, err := lifecycle.TonTransactions(ctx, ownerID, domain.StarsTransactionQuery{
		Limit: 10, Direction: domain.StarsTransactionDirectionOutgoing, Ascending: true,
	})
	if err != nil {
		t.Fatalf("personal ton outgoing: %v", err)
	}
	assertTonTransactionAmounts(t, tonOutgoing.Transactions, []int64{-40, -10})

	channelIncoming1, err := lifecycle.ChannelStarsTransactions(ctx, channelID, domain.StarsTransactionQuery{
		Limit: 1, Direction: domain.StarsTransactionDirectionIncoming,
	})
	if err != nil {
		t.Fatalf("channel stars incoming page1: %v", err)
	}
	assertPostgresStarsAmounts(t, channelIncoming1.Transactions, []int64{20})
	if channelIncoming1.NextOffset == "" {
		t.Fatal("channel stars incoming page1 missing next offset")
	}
	channelIncoming2, err := lifecycle.ChannelStarsTransactions(ctx, channelID, domain.StarsTransactionQuery{
		Offset: channelIncoming1.NextOffset, Limit: 1, Direction: domain.StarsTransactionDirectionIncoming,
	})
	if err != nil {
		t.Fatalf("channel stars incoming page2: %v", err)
	}
	assertPostgresStarsAmounts(t, channelIncoming2.Transactions, []int64{100})
	if channelIncoming2.NextOffset != "" {
		t.Fatalf("channel stars terminal next offset = %q", channelIncoming2.NextOffset)
	}

	channelTonOutgoing, err := lifecycle.ChannelTonTransactions(ctx, channelID, domain.StarsTransactionQuery{
		Limit: 10, Direction: domain.StarsTransactionDirectionOutgoing,
	})
	if err != nil {
		t.Fatalf("channel ton outgoing: %v", err)
	}
	assertTonTransactionAmounts(t, channelTonOutgoing.Transactions, []int64{-10, -40})
}

func assertPostgresStarsAmounts(t *testing.T, transactions []domain.StarsTransaction, want []int64) {
	t.Helper()
	if len(transactions) != len(want) {
		t.Fatalf("stars transaction count = %d, want %d: %+v", len(transactions), len(want), transactions)
	}
	for i, amount := range want {
		if transactions[i].Amount != amount {
			t.Fatalf("stars transaction[%d].amount = %d, want %d", i, transactions[i].Amount, amount)
		}
	}
}

func assertTonTransactionAmounts(t *testing.T, transactions []domain.TonTransaction, want []int64) {
	t.Helper()
	if len(transactions) != len(want) {
		t.Fatalf("ton transaction count = %d, want %d: %+v", len(transactions), len(want), transactions)
	}
	for i, amount := range want {
		if transactions[i].Amount != amount {
			t.Fatalf("ton transaction[%d].amount = %d, want %d", i, transactions[i].Amount, amount)
		}
	}
}
