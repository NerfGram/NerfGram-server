package postgres

import (
	"context"
	"errors"
	"testing"

	"telesrv/internal/domain"
)

type starsPurchaseAttempt struct {
	result domain.StarsPurchaseResult
	err    error
}

func purchaseStarsTwiceConcurrently(t *testing.T, ctx context.Context, store *StarsPurchaseStore, req domain.StarsPurchaseRequest) (domain.StarsPurchaseResult, domain.StarsPurchaseResult) {
	t.Helper()
	start := make(chan struct{})
	attempts := make(chan starsPurchaseAttempt, 2)
	for range 2 {
		go func() {
			<-start
			result, err := store.PurchaseStars(ctx, req)
			attempts <- starsPurchaseAttempt{result: result, err: err}
		}()
	}
	close(start)

	var first, replay domain.StarsPurchaseResult
	firstCount, replayCount := 0, 0
	for range 2 {
		attempt := <-attempts
		if attempt.err != nil {
			t.Fatalf("concurrent Stars purchase: %v", attempt.err)
		}
		if attempt.result.Duplicate {
			replay, replayCount = attempt.result, replayCount+1
		} else {
			first, firstCount = attempt.result, firstCount+1
		}
	}
	if firstCount != 1 || replayCount != 1 {
		t.Fatalf("concurrent Stars purchase first/replay counts = %d/%d, want 1/1", firstCount, replayCount)
	}
	return first, replay
}

func TestStarsFriendGiftPurchaseAtomicReplayAndValidationPostgres(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	users := NewUserStore(pool)
	suffix := randomSuffix(t)
	buyer, err := users.Create(ctx, domain.User{AccessHash: 94101, Phone: "+1665941" + suffix + "01", FirstName: "GiftBuyer"})
	if err != nil {
		t.Fatalf("create buyer: %v", err)
	}
	recipient, err := users.Create(ctx, domain.User{AccessHash: 94102, Phone: "+1665941" + suffix + "02", FirstName: "GiftRecipient"})
	if err != nil {
		t.Fatalf("create recipient: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM stars_purchase_commands WHERE buyer_user_id=$1", buyer.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM stars_purchase_forms WHERE buyer_user_id=$1", buyer.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM stars_transactions WHERE user_id=$1", recipient.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM stars_balances WHERE user_id=$1", recipient.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=ANY($1::bigint[])", []int64{buyer.ID, recipient.ID})
	})

	messages := NewMessageStore(pool)
	store := NewStarsPurchaseStore(pool, messages)
	issued, err := store.IssueStarsPurchaseForm(ctx, domain.StarsPurchaseForm{
		Kind: domain.StarsPurchaseGift, BuyerUserID: buyer.ID, RecipientUserID: recipient.ID,
		Stars: 2500, Currency: "USD", Amount: 199,
		IssuedAt: 1_700_000_000, ExpiresAt: 1_700_000_600,
	})
	if err != nil || issued.FormID == 0 {
		t.Fatalf("issue form = %+v err=%v", issued, err)
	}
	var origin [8]byte
	origin[0] = 9
	req := domain.StarsPurchaseRequest{
		StarsPurchaseForm: domain.StarsPurchaseForm{
			FormID: issued.FormID, Kind: domain.StarsPurchaseGift, BuyerUserID: buyer.ID, RecipientUserID: recipient.ID,
			Stars: 2500, Currency: "USD", Amount: 199,
		},
		Date: 1_700_000_100, OriginAuthKeyID: origin, OriginSessionID: 77,
	}
	first, replay := purchaseStarsTwiceConcurrently(t, ctx, store, req)
	if first.Duplicate || first.Balance.Balance != 2500 || first.TransactionID == "" ||
		first.Send.SenderEvent.PtsCount != 1 || first.Send.RecipientEvent.PtsCount != 1 {
		t.Fatalf("first purchase = %+v", first)
	}
	if first.Send.SenderMessage.Pts <= 0 || first.Send.RecipientMessage.Pts <= 0 ||
		first.Send.SenderMessage.UID == 0 || first.Send.SenderMessage.UID != first.Send.RecipientMessage.UID {
		t.Fatalf("bilateral send = %+v", first.Send)
	}
	action := first.Send.RecipientMessage.Media.ServiceAction.GiftStars
	if action == nil || action.Stars != 2500 || action.Currency != "USD" || action.Amount != 199 ||
		action.TransactionID != first.TransactionID || action.BalanceAfter != 2500 {
		t.Fatalf("recipient gift action = %+v", action)
	}

	if !replay.Duplicate || replay.TransactionID != first.TransactionID ||
		replay.Send.SenderMessage.ID != first.Send.SenderMessage.ID || replay.Send.SenderEvent.Pts != first.Send.SenderEvent.Pts {
		t.Fatalf("replay = %+v, first=%+v", replay, first)
	}

	var balance, txnCount, commandCount int64
	if err := pool.QueryRow(ctx, "SELECT balance FROM stars_balances WHERE user_id=$1", recipient.ID).Scan(&balance); err != nil {
		t.Fatalf("load recipient balance: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM stars_transactions WHERE user_id=$1 AND reason='gift'", recipient.ID).Scan(&txnCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM stars_purchase_commands WHERE buyer_user_id=$1", buyer.ID).Scan(&commandCount); err != nil {
		t.Fatal(err)
	}
	if balance != 2500 || txnCount != 1 || commandCount != 1 {
		t.Fatalf("replay footprint balance=%d txns=%d commands=%d", balance, txnCount, commandCount)
	}
	for _, userID := range []int64{buyer.ID, recipient.ID} {
		var eventCount, outboxCount int64
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM user_update_events WHERE user_id=$1", userID).Scan(&eventCount); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM dispatch_outbox WHERE target_user_id=$1", userID).Scan(&outboxCount); err != nil {
			t.Fatal(err)
		}
		if eventCount != 1 || outboxCount != 1 {
			t.Fatalf("user %d event/outbox=%d/%d, want 1/1", userID, eventCount, outboxCount)
		}
	}

	tampered := req
	tampered.Amount++
	if _, err := store.PurchaseStars(ctx, tampered); !errors.Is(err, domain.ErrStarsPurchaseFormInvalid) {
		t.Fatalf("tampered replay err=%v", err)
	}
	expired, err := store.IssueStarsPurchaseForm(ctx, domain.StarsPurchaseForm{
		Kind: domain.StarsPurchaseGift, BuyerUserID: buyer.ID, RecipientUserID: recipient.ID,
		Stars: 1000, Currency: "USD", Amount: 99,
		IssuedAt: 1_699_999_000, ExpiresAt: 1_699_999_600,
	})
	if err != nil {
		t.Fatalf("issue expired form: %v", err)
	}
	expiredReq := req
	expiredReq.FormID, expiredReq.Stars, expiredReq.Amount = expired.FormID, 1000, 99
	if _, err := store.PurchaseStars(ctx, expiredReq); !errors.Is(err, domain.ErrStarsPurchaseFormExpired) {
		t.Fatalf("expired form err=%v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT balance FROM stars_balances WHERE user_id=$1", recipient.ID).Scan(&balance); err != nil || balance != 2500 {
		t.Fatalf("balance after failures=%d err=%v", balance, err)
	}
}

func TestStarsTopupPurchaseAtomicReplayAndPurposeBindingPostgres(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	users := NewUserStore(pool)
	suffix := randomSuffix(t)
	buyer, err := users.Create(ctx, domain.User{AccessHash: 94201, Phone: "+1665942" + suffix + "01", FirstName: "TopupBuyer"})
	if err != nil {
		t.Fatalf("create buyer: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM stars_purchase_commands WHERE buyer_user_id=$1", buyer.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM stars_purchase_forms WHERE buyer_user_id=$1", buyer.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM stars_transactions WHERE user_id=$1", buyer.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM stars_balances WHERE user_id=$1", buyer.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", buyer.ID)
	})

	store := NewStarsPurchaseStore(pool, nil)
	purposePeer := domain.Peer{Type: domain.PeerTypeUser, ID: buyer.ID + 100}
	issued, err := store.IssueStarsPurchaseForm(ctx, domain.StarsPurchaseForm{
		Kind: domain.StarsPurchaseTopup, BuyerUserID: buyer.ID, SpendPurposePeer: purposePeer,
		Stars: 2500, Currency: "USD", Amount: 199,
		IssuedAt: 1_700_000_000, ExpiresAt: 1_700_000_600,
	})
	if err != nil || issued.FormID == 0 {
		t.Fatalf("issue form = %+v err=%v", issued, err)
	}
	req := domain.StarsPurchaseRequest{
		StarsPurchaseForm: domain.StarsPurchaseForm{
			FormID: issued.FormID, Kind: domain.StarsPurchaseTopup, BuyerUserID: buyer.ID,
			SpendPurposePeer: purposePeer, Stars: 2500, Currency: "USD", Amount: 199,
		},
		Date: 1_700_000_100,
	}
	first, replay := purchaseStarsTwiceConcurrently(t, ctx, store, req)
	if first.Duplicate || first.Balance.Balance != 2500 || first.TransactionID == "" {
		t.Fatalf("first purchase = %+v", first)
	}
	if !replay.Duplicate || replay.Balance.Balance != 2500 || replay.TransactionID != first.TransactionID {
		t.Fatalf("replay = %+v, first=%+v", replay, first)
	}

	var balance, txnCount, commandCount int64
	if err := pool.QueryRow(ctx, "SELECT balance FROM stars_balances WHERE user_id=$1", buyer.ID).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM stars_transactions WHERE user_id=$1 AND reason='topup'", buyer.ID).Scan(&txnCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM stars_purchase_commands WHERE buyer_user_id=$1", buyer.ID).Scan(&commandCount); err != nil {
		t.Fatal(err)
	}
	if balance != 2500 || txnCount != 1 || commandCount != 1 {
		t.Fatalf("replay footprint balance=%d txns=%d commands=%d", balance, txnCount, commandCount)
	}

	tampered := req
	tampered.SpendPurposePeer.ID++
	if _, err := store.PurchaseStars(ctx, tampered); !errors.Is(err, domain.ErrStarsPurchaseFormInvalid) {
		t.Fatalf("tampered purpose replay err=%v", err)
	}
	otherBuyer := req
	otherBuyer.BuyerUserID++
	if _, err := store.PurchaseStars(ctx, otherBuyer); !errors.Is(err, domain.ErrStarsPurchaseFormInvalid) {
		t.Fatalf("cross-account form err=%v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT balance FROM stars_balances WHERE user_id=$1", buyer.ID).Scan(&balance); err != nil || balance != 2500 {
		t.Fatalf("balance after invalid submissions=%d err=%v", balance, err)
	}
}

func TestStarsGiveawayPurchaseAtomicChannelPTSReplayAndInfoPostgres(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	users := NewUserStore(pool)
	suffix := randomSuffix(t)
	owner, err := users.Create(ctx, domain.User{AccessHash: 94301, Phone: "+1665943" + suffix + "01", FirstName: "GiveawayOwner", CountryCode: "1"})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	member, err := users.Create(ctx, domain.User{AccessHash: 94302, Phone: "+1665943" + suffix + "02", FirstName: "GiveawayMember", CountryCode: "1"})
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	channels := NewChannelStore(pool)
	created, err := channels.CreateChannel(ctx, domain.CreateChannelRequest{
		CreatorUserID: owner.ID, Title: "Stars Giveaway " + suffix, Megagroup: true,
		MemberUserIDs: []int64{member.ID}, Date: 1_700_000_000,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	channelID := created.Channel.ID
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM stars_giveaways WHERE buyer_user_id=$1", owner.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM stars_purchase_commands WHERE buyer_user_id=$1", owner.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM stars_purchase_forms WHERE buyer_user_id=$1", owner.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM channels WHERE id=$1", channelID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=ANY($1::bigint[])", []int64{owner.ID, member.ID})
	})
	before, err := channels.GetChannelByID(ctx, channelID)
	if err != nil {
		t.Fatal(err)
	}
	purpose := &domain.StarsGiveawayPurchase{
		BoostPeer:     domain.Peer{Type: domain.PeerTypeChannel, ID: channelID},
		CountriesISO2: []string{"US"}, RandomID: 9430001, UntilDate: 1_700_003_700,
		Users: 2, PerUserStars: 500, YearlyBoosts: 4, WinnersAreVisible: true,
	}
	store := NewStarsPurchaseStore(pool, nil, channels)
	issued, err := store.IssueStarsPurchaseForm(ctx, domain.StarsPurchaseForm{
		Kind: domain.StarsPurchaseGiveaway, BuyerUserID: owner.ID, Giveaway: purpose,
		Stars: 1000, Currency: "USD", Amount: 99, IssuedAt: 1_700_000_100, ExpiresAt: 1_700_000_700,
	})
	if err != nil || issued.FormID == 0 {
		t.Fatalf("issue giveaway form=%+v err=%v", issued, err)
	}
	req := domain.StarsPurchaseRequest{StarsPurchaseForm: domain.StarsPurchaseForm{
		FormID: issued.FormID, Kind: domain.StarsPurchaseGiveaway, BuyerUserID: owner.ID, Giveaway: purpose,
		Stars: 1000, Currency: "USD", Amount: 99,
	}, Date: 1_700_000_200}
	first, replay := purchaseStarsTwiceConcurrently(t, ctx, store, req)
	if first.Duplicate || first.TransactionID == "" || first.ChannelSend.Event.PtsCount != 1 ||
		first.ChannelSend.Event.Pts != before.Pts+1 || first.ChannelSend.Message.Media == nil ||
		first.ChannelSend.Message.Media.Giveaway == nil {
		t.Fatalf("first giveaway result=%+v before_pts=%d", first, before.Pts)
	}
	media := first.ChannelSend.Message.Media.Giveaway
	if media.Stars != 1000 || media.Quantity != 2 || len(media.Channels) != 1 || media.Channels[0] != channelID ||
		media.UntilDate != purpose.UntilDate || !media.WinnersAreVisible {
		t.Fatalf("giveaway media=%+v", media)
	}
	difference, err := channels.ListChannelDifference(ctx, domain.ChannelDifferenceRequest{
		UserID: member.ID, ChannelID: channelID, Pts: before.Pts, Limit: 10,
	})
	if err != nil || difference.Pts != first.ChannelSend.Event.Pts || len(difference.Events) != 1 || len(difference.NewMessages) != 1 ||
		difference.NewMessages[0].Media == nil || difference.NewMessages[0].Media.Giveaway == nil ||
		difference.NewMessages[0].Media.Giveaway.Stars != 1000 {
		t.Fatalf("giveaway channel difference=%+v err=%v", difference, err)
	}
	if !replay.Duplicate || replay.TransactionID != first.TransactionID ||
		replay.ChannelSend.Message.ID != first.ChannelSend.Message.ID || replay.ChannelSend.Event.Pts != first.ChannelSend.Event.Pts {
		t.Fatalf("giveaway replay=%+v first=%+v", replay, first)
	}
	lateReplayReq := req
	lateReplayReq.Date = purpose.UntilDate
	lateReplay, err := store.PurchaseStars(ctx, lateReplayReq)
	if err != nil || !lateReplay.Duplicate || lateReplay.TransactionID != first.TransactionID ||
		lateReplay.ChannelSend.Message.ID != first.ChannelSend.Message.ID || lateReplay.ChannelSend.Event.Pts != first.ChannelSend.Event.Pts {
		t.Fatalf("giveaway replay after until_date=%+v err=%v first=%+v", lateReplay, err, first)
	}

	latePurpose := *purpose
	latePurpose.RandomID++
	lateForm, err := store.IssueStarsPurchaseForm(ctx, domain.StarsPurchaseForm{
		Kind: domain.StarsPurchaseGiveaway, BuyerUserID: owner.ID, Giveaway: &latePurpose,
		Stars: 1000, Currency: "USD", Amount: 99,
		IssuedAt: purpose.UntilDate - 100, ExpiresAt: purpose.UntilDate + 500,
	})
	if err != nil {
		t.Fatalf("issue giveaway form before until_date: %v", err)
	}
	lateFirstReq := req
	lateFirstReq.FormID = lateForm.FormID
	lateFirstReq.Giveaway = &latePurpose
	lateFirstReq.Date = purpose.UntilDate
	if _, err := store.PurchaseStars(ctx, lateFirstReq); !errors.Is(err, domain.ErrStarsPurchaseFormExpired) {
		t.Fatalf("first giveaway settlement at until_date err=%v, want form expired", err)
	}
	var campaigns, commands, messages, events, balanceRows, txns int64
	queries := []struct {
		query  string
		args   []any
		target *int64
	}{
		{"SELECT count(*) FROM stars_giveaways WHERE buyer_user_id=$1", []any{owner.ID}, &campaigns},
		{"SELECT count(*) FROM stars_purchase_commands WHERE buyer_user_id=$1", []any{owner.ID}, &commands},
		{"SELECT count(*) FROM channel_messages WHERE channel_id=$1 AND id=$2", []any{channelID, first.ChannelSend.Message.ID}, &messages},
		{"SELECT count(*) FROM channel_update_events WHERE channel_id=$1 AND pts=$2", []any{channelID, first.ChannelSend.Event.Pts}, &events},
		{"SELECT count(*) FROM stars_balances WHERE user_id=$1", []any{owner.ID}, &balanceRows},
		{"SELECT count(*) FROM stars_transactions WHERE user_id=$1", []any{owner.ID}, &txns},
	}
	for _, item := range queries {
		if err := pool.QueryRow(ctx, item.query, item.args...).Scan(item.target); err != nil {
			t.Fatalf("footprint query %q: %v", item.query, err)
		}
	}
	if campaigns != 1 || commands != 1 || messages != 1 || events != 1 || balanceRows != 0 || txns != 0 {
		t.Fatalf("footprint campaigns=%d commands=%d messages=%d events=%d balances=%d txns=%d", campaigns, commands, messages, events, balanceRows, txns)
	}
	ownerInfo, err := store.GetStarsGiveawayInfo(ctx, owner.ID, channelID, first.ChannelSend.Message.ID, 1_700_000_300)
	if err != nil || ownerInfo.AdminDisallowedChatID != channelID || ownerInfo.Participating {
		t.Fatalf("owner giveaway info=%+v err=%v", ownerInfo, err)
	}
	memberInfo, err := store.GetStarsGiveawayInfo(ctx, member.ID, channelID, first.ChannelSend.Message.ID, 1_700_000_300)
	if err != nil || !memberInfo.Participating || memberInfo.StartDate != req.Date {
		t.Fatalf("member giveaway info=%+v err=%v", memberInfo, err)
	}
	preparing, err := store.GetStarsGiveawayInfo(ctx, member.ID, channelID, first.ChannelSend.Message.ID, purpose.UntilDate)
	if err != nil || !preparing.PreparingResults || preparing.Participating {
		t.Fatalf("preparing giveaway info=%+v err=%v", preparing, err)
	}
	tampered := req
	changed := *purpose
	changed.Users, changed.PerUserStars = 1, 1000
	tampered.Giveaway = &changed
	if _, err := store.PurchaseStars(ctx, tampered); !errors.Is(err, domain.ErrStarsPurchaseFormInvalid) {
		t.Fatalf("tampered giveaway replay err=%v", err)
	}
}
