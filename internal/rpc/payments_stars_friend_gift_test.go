package rpc

import (
	"context"
	"testing"

	"github.com/iamxvbaba/td/clock"
	"github.com/iamxvbaba/td/tg"
	"github.com/iamxvbaba/td/tgerr"
	"go.uber.org/zap/zaptest"

	appstars "telesrv/internal/app/stars"
	appusers "telesrv/internal/app/users"
	"telesrv/internal/domain"
	"telesrv/internal/store/memory"
)

type starsFriendGiftRPCStore struct {
	*memory.StarsStore
	issued    domain.StarsPurchaseForm
	purchased domain.StarsPurchaseRequest
	purchases int
}

func (s *starsFriendGiftRPCStore) IssueStarsPurchaseForm(_ context.Context, form domain.StarsPurchaseForm) (domain.StarsPurchaseForm, error) {
	form.FormID = 70001
	s.issued = form
	return form, nil
}

func (s *starsFriendGiftRPCStore) PurchaseStars(_ context.Context, req domain.StarsPurchaseRequest) (domain.StarsPurchaseResult, error) {
	s.purchased = req
	s.purchases++
	action := &domain.MessageServiceAction{Kind: domain.MessageServiceActionGiftStars, GiftStars: &domain.MessageGiftStarsAction{
		Currency: req.Currency, Amount: req.Amount, Stars: req.Stars,
		TransactionID: "stars-gift-test", BalanceAfter: 4321,
	}}
	sender := domain.Message{ID: 11, OwnerUserID: req.BuyerUserID, Peer: domain.Peer{Type: domain.PeerTypeUser, ID: req.RecipientUserID},
		From: domain.Peer{Type: domain.PeerTypeUser, ID: req.BuyerUserID}, Out: true, Date: req.Date,
		Media: &domain.MessageMedia{Kind: domain.MessageMediaKindService, ServiceAction: action}}
	recipient := sender
	recipient.ID, recipient.OwnerUserID, recipient.Peer, recipient.Out = 12, req.RecipientUserID,
		domain.Peer{Type: domain.PeerTypeUser, ID: req.BuyerUserID}, false
	return domain.StarsPurchaseResult{
		Balance:       domain.StarsBalance{UserID: req.RecipientUserID, Balance: 4321},
		TransactionID: "stars-gift-test",
		Send: domain.SendPrivateTextResult{
			SenderMessage: sender, RecipientMessage: recipient,
			SenderEvent:    domain.UpdateEvent{UserID: req.BuyerUserID, Type: domain.UpdateEventNewMessage, Pts: 5, PtsCount: 1, Date: req.Date, Message: sender},
			RecipientEvent: domain.UpdateEvent{UserID: req.RecipientUserID, Type: domain.UpdateEventNewMessage, Pts: 9, PtsCount: 1, Date: req.Date, Message: recipient},
		},
	}, nil
}

func starsFriendGiftTestRouter(t *testing.T) (*Router, *starsFriendGiftRPCStore, domain.User, domain.User) {
	t.Helper()
	ctx := context.Background()
	users := memory.NewUserStore()
	buyer, err := users.Create(ctx, domain.User{AccessHash: 8101, Phone: "+15558101", FirstName: "Buyer"})
	if err != nil {
		t.Fatal(err)
	}
	recipient, err := users.Create(ctx, domain.User{AccessHash: 8102, Phone: "+15558102", FirstName: "Recipient"})
	if err != nil {
		t.Fatal(err)
	}
	st := &starsFriendGiftRPCStore{StarsStore: memory.NewStarsStore()}
	r := New(Config{DC: 2, PublicBaseURL: "https://links.example.test"}, Deps{
		Users: appusers.NewService(users),
		Stars: appstars.NewService(st, appstars.WithStartingGrant(0), appstars.WithPurchaseStore(st)),
	}, zaptest.NewLogger(t), clock.System)
	return r, st, buyer, recipient
}

func TestStarsFriendGiftOptionsFormAndFiatSettlement(t *testing.T) {
	r, st, buyer, recipient := starsFriendGiftTestRouter(t)
	ctx := WithUserID(context.Background(), buyer.ID)

	generic, err := r.onPaymentsGetStarsGiftOptions(ctx, &tg.PaymentsGetStarsGiftOptionsRequest{})
	if err != nil || len(generic) != 3 || generic[0].Stars != 1000 || generic[0].Currency != "USD" || generic[0].Amount != 99 {
		t.Fatalf("generic gift options = %+v err=%v", generic, err)
	}
	input := &tg.InputUser{UserID: recipient.ID, AccessHash: recipient.AccessHash}
	personalReq := &tg.PaymentsGetStarsGiftOptionsRequest{}
	personalReq.SetUserID(input)
	personal, err := r.onPaymentsGetStarsGiftOptions(ctx, personalReq)
	if err != nil || len(personal) != len(generic) {
		t.Fatalf("personal gift options = %+v err=%v", personal, err)
	}

	purpose := &tg.InputStorePaymentStarsGift{UserID: input, Stars: 2500, Currency: "USD", Amount: 199}
	invoice := &tg.InputInvoiceStars{Purpose: purpose}
	formClass, err := r.onPaymentsGetPaymentForm(ctx, &tg.PaymentsGetPaymentFormRequest{Invoice: invoice})
	if err != nil {
		t.Fatalf("get gift payment form: %v", err)
	}
	form, ok := formClass.(*tg.PaymentsPaymentForm)
	if !ok || form.FormID != 70001 || !form.Invoice.Test || form.Invoice.Currency != "USD" ||
		len(form.Invoice.Prices) != 1 || form.Invoice.Prices[0].Amount != 199 ||
		form.ProviderID != domain.OfficialSystemUserID || form.URL != "https://links.example.test/payments/dev-stars?form_id=70001" {
		t.Fatalf("gift payment form = %T %+v", formClass, formClass)
	}
	if st.issued.Kind != domain.StarsPurchaseGift || st.issued.BuyerUserID != buyer.ID || st.issued.RecipientUserID != recipient.ID || st.issued.Stars != 2500 || st.issued.ExpiresAt != st.issued.IssuedAt+600 {
		t.Fatalf("issued form = %+v", st.issued)
	}

	if _, err := r.onPaymentsSendStarsForm(ctx, &tg.PaymentsSendStarsFormRequest{FormID: form.FormID, Invoice: invoice}); !tgerr.Is(err, "PAYMENT_CREDENTIALS_INVALID") {
		t.Fatalf("sendStarsForm fiat gift err = %v, want PAYMENT_CREDENTIALS_INVALID", err)
	}
	resultClass, err := r.onPaymentsSendPaymentForm(ctx, &tg.PaymentsSendPaymentFormRequest{
		FormID: form.FormID, Invoice: invoice, Credentials: devStarsCredentials(form.FormID),
	})
	if err != nil {
		t.Fatalf("sendPaymentForm gift: %v", err)
	}
	result, ok := resultClass.(*tg.PaymentsPaymentResult)
	if !ok {
		t.Fatalf("sendStarsForm result = %T", resultClass)
	}
	updates, ok := result.Updates.(*tg.Updates)
	if !ok || len(updates.Updates) != 1 {
		t.Fatalf("sender updates = %T %+v", result.Updates, result.Updates)
	}
	newMessage, ok := updates.Updates[0].(*tg.UpdateNewMessage)
	if !ok || newMessage.Pts != 5 || newMessage.PtsCount != 1 {
		t.Fatalf("sender new message = %T %+v", updates.Updates[0], updates.Updates[0])
	}
	serviceMessage, ok := newMessage.Message.(*tg.MessageService)
	if !ok {
		t.Fatalf("gift message = %T", newMessage.Message)
	}
	action, ok := serviceMessage.Action.(*tg.MessageActionGiftStars)
	if !ok || action.Stars != 2500 || action.Currency != "USD" || action.Amount != 199 || action.TransactionID != "" {
		t.Fatalf("sender gift action = %T %+v", serviceMessage.Action, serviceMessage.Action)
	}
	if st.purchased.FormID != form.FormID || st.purchased.RecipientUserID != recipient.ID || st.purchases != 1 {
		t.Fatalf("purchase request = %+v count=%d", st.purchased, st.purchases)
	}

	if st.purchases != 1 {
		t.Fatalf("settlement count = %d, want one fiat submit", st.purchases)
	}
}

func TestStarsDirectPurchaseValidateRequestedInfoIsReadOnly(t *testing.T) {
	r, st, buyer, recipient := starsFriendGiftTestRouter(t)
	ctx := WithUserID(context.Background(), buyer.ID)

	topup := &tg.InputInvoiceStars{Purpose: &tg.InputStorePaymentStarsTopup{
		Stars: 1000, Currency: "USD", Amount: 99,
	}}
	gift := &tg.InputInvoiceStars{Purpose: &tg.InputStorePaymentStarsGift{
		UserID: &tg.InputUser{UserID: recipient.ID, AccessHash: recipient.AccessHash},
		Stars:  2500, Currency: "USD", Amount: 199,
	}}
	for name, invoice := range map[string]tg.InputInvoiceClass{"topup": topup, "gift": gift} {
		result, err := r.onPaymentsValidateRequestedInfo(ctx, &tg.PaymentsValidateRequestedInfoRequest{
			Save: true, Invoice: invoice,
		})
		if err != nil {
			t.Fatalf("%s validateRequestedInfo: %v", name, err)
		}
		if result == nil || !result.Zero() {
			t.Fatalf("%s validated info = %+v, want flags=0", name, result)
		}
	}
	if st.issued.FormID != 0 || st.purchases != 0 {
		t.Fatalf("validation mutated purchase store: issued=%+v purchases=%d", st.issued, st.purchases)
	}

	withInfo := &tg.PaymentsValidateRequestedInfoRequest{Invoice: topup}
	withInfo.Info.SetName("unexpected")
	if _, err := r.onPaymentsValidateRequestedInfo(ctx, withInfo); !tgerr.Is(err, "REQUESTED_INFO_INVALID") {
		t.Fatalf("non-empty info err=%v, want REQUESTED_INFO_INVALID", err)
	}
	if _, err := r.onPaymentsValidateRequestedInfo(ctx, &tg.PaymentsValidateRequestedInfoRequest{
		Invoice: &tg.InputInvoiceSlug{Slug: "unsupported"},
	}); !tgerr.Is(err, "NOT_IMPLEMENTED") {
		t.Fatalf("non-Stars invoice err=%v, want NOT_IMPLEMENTED", err)
	}
	if _, err := r.onPaymentsValidateRequestedInfo(ctx, &tg.PaymentsValidateRequestedInfoRequest{
		Invoice: &tg.InputInvoiceStars{Purpose: &tg.InputStorePaymentStarsTopup{Stars: 1000, Currency: "USD", Amount: 100}},
	}); !tgerr.Is(err, "STARS_FORM_AMOUNT_MISMATCH") {
		t.Fatalf("tampered package err=%v, want STARS_FORM_AMOUNT_MISMATCH", err)
	}
	if st.issued.FormID != 0 || st.purchases != 0 {
		t.Fatalf("invalid validation mutated purchase store: issued=%+v purchases=%d", st.issued, st.purchases)
	}
}

func TestStarsFriendGiftRejectsInvalidRecipientAndPackageBeforeStore(t *testing.T) {
	r, st, buyer, recipient := starsFriendGiftTestRouter(t)
	ctx := WithUserID(context.Background(), buyer.ID)

	badRecipientReq := &tg.PaymentsGetStarsGiftOptionsRequest{}
	badRecipientReq.SetUserID(&tg.InputUser{UserID: recipient.ID + 99999})
	if _, err := r.onPaymentsGetStarsGiftOptions(ctx, badRecipientReq); !tgerr.Is(err, "USER_ID_INVALID") {
		t.Fatalf("bad recipient err = %v", err)
	}
	bad := &tg.InputInvoiceStars{Purpose: &tg.InputStorePaymentStarsGift{
		UserID: &tg.InputUser{UserID: recipient.ID, AccessHash: recipient.AccessHash},
		Stars:  2500, Currency: "USD", Amount: 200,
	}}
	if _, err := r.onPaymentsGetPaymentForm(ctx, &tg.PaymentsGetPaymentFormRequest{Invoice: bad}); !tgerr.Is(err, "STARS_FORM_AMOUNT_MISMATCH") {
		t.Fatalf("tampered package form err = %v", err)
	}
	if _, err := r.onPaymentsSendPaymentForm(ctx, &tg.PaymentsSendPaymentFormRequest{
		FormID: 70001, Invoice: bad, Credentials: devStarsCredentials(70001),
	}); !tgerr.Is(err, "STARS_FORM_AMOUNT_MISMATCH") {
		t.Fatalf("tampered package settle err = %v", err)
	}
	if st.issued.FormID != 0 || st.purchases != 0 {
		t.Fatalf("invalid request reached store: issued=%+v purchases=%d", st.issued, st.purchases)
	}
}

func TestAndroidStorePurchaseFailsClosedInFavorOfInvoiceCheckout(t *testing.T) {
	r, _, buyer, recipient := starsFriendGiftTestRouter(t)
	ctx := WithUserID(context.Background(), buyer.ID)
	purpose := &tg.InputStorePaymentStarsGift{
		UserID: &tg.InputUser{UserID: recipient.ID, AccessHash: recipient.AccessHash},
		Stars:  1000, Currency: "USD", Amount: 99,
	}
	allowed, err := r.onPaymentsCanPurchaseStore(ctx, &tg.PaymentsCanPurchaseStoreRequest{Purpose: purpose})
	if err != nil {
		t.Fatalf("canPurchaseStore: %v", err)
	}
	if allowed {
		t.Fatal("canPurchaseStore = true, want false")
	}
	if _, err := r.onPaymentsAssignPlayMarketTransaction(ctx, &tg.PaymentsAssignPlayMarketTransactionRequest{
		Receipt: tg.DataJSON{Data: `{"orderId":"unverified"}`}, Purpose: purpose,
	}); !tgerr.Is(err, "STORE_PAYMENT_UNAVAILABLE") {
		t.Fatalf("assignPlayMarketTransaction err = %v, want STORE_PAYMENT_UNAVAILABLE", err)
	}
}

func TestGiftStarsRecipientProjectionCarriesBalanceOnlineAndDifference(t *testing.T) {
	action := &domain.MessageServiceAction{Kind: domain.MessageServiceActionGiftStars, GiftStars: &domain.MessageGiftStarsAction{
		Currency: "USD", Amount: 99, Stars: 1000, TransactionID: "txn-1", BalanceAfter: 3100,
	}}
	msg := domain.Message{ID: 4, OwnerUserID: 2, Peer: domain.Peer{Type: domain.PeerTypeUser, ID: 1},
		From: domain.Peer{Type: domain.PeerTypeUser, ID: 1}, Date: 1700000000,
		Media: &domain.MessageMedia{Kind: domain.MessageMediaKindService, ServiceAction: action}}
	event := domain.UpdateEvent{UserID: 2, Type: domain.UpdateEventNewMessage, Pts: 8, PtsCount: 1, Date: msg.Date, Message: msg}
	online := tgPrivateMessageUpdates(event, msg, 0, false, nil, nil)
	if len(online.Updates) != 2 {
		t.Fatalf("online updates = %+v", online.Updates)
	}
	balance, ok := online.Updates[1].(*tg.UpdateStarsBalance)
	if !ok || balance.Balance.(*tg.StarsAmount).Amount != 3100 {
		t.Fatalf("online balance = %T %+v", online.Updates[1], online.Updates[1])
	}
	diff := tgUpdatesDifference(2, domain.UpdateDifference{Events: []domain.UpdateEvent{event}, State: domain.UpdateState{Pts: 8}})
	full, ok := diff.(*tg.UpdatesDifference)
	if !ok || len(full.NewMessages) != 1 || len(full.OtherUpdates) != 1 {
		t.Fatalf("difference = %T %+v", diff, diff)
	}
	if _, ok := full.OtherUpdates[0].(*tg.UpdateStarsBalance); !ok {
		t.Fatalf("difference balance = %T", full.OtherUpdates[0])
	}
}
