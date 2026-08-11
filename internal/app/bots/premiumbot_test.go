package bots

import (
	"context"
	"strings"
	"testing"

	"telesrv/internal/domain"
	"telesrv/internal/store/memory"
)

type fakePremiumStorefront struct {
	plans   []domain.PremiumPlan
	active  []domain.PremiumEntitlement
	history []domain.PremiumEntitlement
	balance int64
}

func (f *fakePremiumStorefront) BotUserID() int64    { return domain.PremiumBotConfiguredUserID() }
func (f *fakePremiumStorefront) BotUsername() string { return "premiumbot" }
func (f *fakePremiumStorefront) Plans(context.Context) ([]domain.PremiumPlan, error) {
	return append([]domain.PremiumPlan(nil), f.plans...), nil
}
func (f *fakePremiumStorefront) Plan(_ context.Context, months int) (domain.PremiumPlan, error) {
	for _, plan := range f.plans {
		if plan.Months == months {
			return plan, nil
		}
	}
	return domain.PremiumPlan{}, domain.ErrPremiumPlanUnavailable
}
func (f *fakePremiumStorefront) Balance(context.Context, int64) (domain.StarsBalance, error) {
	return domain.StarsBalance{Balance: f.balance}, nil
}
func (f *fakePremiumStorefront) ActiveEntitlements(context.Context, int64, int) ([]domain.PremiumEntitlement, error) {
	return append([]domain.PremiumEntitlement(nil), f.active...), nil
}
func (f *fakePremiumStorefront) PurchaseHistory(context.Context, int64, int) ([]domain.PremiumEntitlement, error) {
	return append([]domain.PremiumEntitlement(nil), f.history...), nil
}

func premiumBotTestPlan() domain.PremiumPlan {
	return domain.PremiumPlan{
		Months: 3, DurationDays: 90, AmountStars: 750, Enabled: true,
		SortOrder: 10, Label: "3 months", Version: 4,
	}
}

func TestPremiumBotStartHintsNeverSelectPrice(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	svc.SetPremium(&fakePremiumStorefront{plans: []domain.PremiumPlan{premiumBotTestPlan()}})
	for _, body := range []string{
		"/start settings", "/start profile", "/start business",
		"/start deeplink_referrer", "/start doubled_limits",
	} {
		reply := svc.handlePremium(context.Background(), 1001, body)
		if reply.Media != nil {
			t.Fatalf("%q produced invoice media from a navigation hint", body)
		}
		if reply.ReplyMarkup == nil || len(reply.ReplyMarkup.Inline) < 3 {
			t.Fatalf("%q did not produce the safe main menu: %+v", body, reply)
		}
	}
}

func TestPremiumBotInvoiceUsesCatalogSnapshotAndBuyButton(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	plan := premiumBotTestPlan()
	svc.SetPremium(&fakePremiumStorefront{plans: []domain.PremiumPlan{plan}, balance: 1250})

	reply := svc.handlePremium(context.Background(), 1001, "/start buy_3")
	if reply.Media == nil || reply.Media.Kind != domain.MessageMediaKindInvoice ||
		reply.Media.Invoice == nil {
		t.Fatalf("invoice reply media = %+v", reply.Media)
	}
	invoice := reply.Media.Invoice
	if invoice.Kind != domain.PremiumPurchaseSelf || invoice.Months != plan.Months ||
		invoice.DurationDays != plan.DurationDays || invoice.AmountStars != plan.AmountStars ||
		invoice.PlanVersion != plan.Version {
		t.Fatalf("invoice = %+v, want catalog snapshot %+v", invoice, plan)
	}
	if !strings.Contains(reply.Text, "Текущий баланс: 1250 Stars") {
		t.Fatalf("invoice text does not show Stars balance: %q", reply.Text)
	}
	if reply.ReplyMarkup == nil || len(reply.ReplyMarkup.Inline) != 1 ||
		len(reply.ReplyMarkup.Inline[0]) != 1 ||
		reply.ReplyMarkup.Inline[0][0].Type != domain.MarkupButtonBuy {
		t.Fatalf("invoice markup = %+v, want one buy button", reply.ReplyMarkup)
	}
	if err := domain.ValidateReplyMarkup(reply.ReplyMarkup); err != nil {
		t.Fatalf("invoice markup validation: %v", err)
	}
}

func TestPremiumBotGiftPickerAndCatalogInvoice(t *testing.T) {
	svc, users, _, _ := newTestService(t)
	buyer := newOwner(t, users, "+15550001")
	recipient := newOwner(t, users, "+15550002")
	plan := premiumBotTestPlan()
	svc.SetPremium(&fakePremiumStorefront{plans: []domain.PremiumPlan{plan}, balance: 2200})

	picker := svc.handlePremium(context.Background(), buyer.ID, "/gift")
	if picker.ReplyMarkup == nil || picker.ReplyMarkup.Type != domain.MessageReplyMarkupKeyboard ||
		len(picker.ReplyMarkup.Keyboard) != 1 ||
		picker.ReplyMarkup.Keyboard[0][0].Type != domain.MarkupButtonRequestPeer {
		t.Fatalf("gift picker = %+v", picker.ReplyMarkup)
	}
	if picker.ReplyMarkup.Selective {
		t.Fatal("gift picker must be visible in a private bot chat")
	}
	if err := domain.ValidateReplyMarkup(picker.ReplyMarkup); err != nil {
		t.Fatalf("gift picker markup: %v", err)
	}

	selected := svc.handlePremiumMessage(context.Background(), buyer.ID, domain.Message{
		Media: &domain.MessageMedia{Kind: domain.MessageMediaKindService,
			ServiceAction: &domain.MessageServiceAction{
				Kind: domain.MessageServiceActionRequestedPeer,
				RequestedPeer: &domain.MessageRequestedPeerAction{
					ButtonID: premiumGiftPeerButtonID,
					Peers:    []domain.Peer{{Type: domain.PeerTypeUser, ID: recipient.ID}},
				},
			}},
	})
	if selected.ReplyMarkup == nil || selected.ReplyMarkup.Type != domain.MessageReplyMarkupInline ||
		len(selected.ReplyMarkup.Inline) != 1 ||
		!strings.HasPrefix(string(selected.ReplyMarkup.Inline[0][0].Data), premiumCallbackGiftBuy) {
		t.Fatalf("selected recipient plans = %+v", selected)
	}
	if err := domain.ValidateReplyMarkup(selected.ReplyMarkup); err != nil {
		t.Fatalf("gift plans markup: %v", err)
	}

	invoiceReply := svc.premiumBotGiftInvoice(
		context.Background(), buyer.ID, recipient.ID, plan.Months, "Happy birthday",
	)
	if invoiceReply.Media == nil || invoiceReply.Media.Invoice == nil {
		t.Fatalf("gift invoice reply = %+v", invoiceReply)
	}
	invoice := invoiceReply.Media.Invoice
	if invoice.Kind != domain.PremiumPurchaseGift || invoice.RecipientUserID != recipient.ID ||
		invoice.Months != plan.Months || invoice.AmountStars != plan.AmountStars ||
		invoice.PlanVersion != plan.Version || invoice.Message.Text != "Happy birthday" {
		t.Fatalf("gift invoice = %+v", invoice)
	}
	if !containsAll(invoiceReply.Text, "Цена: 750 Stars", "Текущий баланс: 2200 Stars") {
		t.Fatalf("gift invoice text does not show price and balance: %q", invoiceReply.Text)
	}
	if err := domain.ValidateReplyMarkup(invoiceReply.ReplyMarkup); err != nil {
		t.Fatalf("gift invoice markup: %v", err)
	}
}

func TestPremiumBotStatusAndHistory(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	now := int(svc.now().Unix())
	svc.SetPremium(&fakePremiumStorefront{
		plans:  []domain.PremiumPlan{premiumBotTestPlan()},
		active: []domain.PremiumEntitlement{{Status: domain.PremiumEntitlementActive, ExpiresAt: now + 86400}},
		history: []domain.PremiumEntitlement{{
			UserID: 2002, SourceUserID: 1001, Source: domain.PremiumEntitlementGift,
			Months: 3, Status: domain.PremiumEntitlementActive, CreatedAt: now,
		}},
	})
	if reply := svc.handlePremium(context.Background(), 1001, "/status"); !containsAll(reply.Text, "активен до", "UTC") {
		t.Fatalf("/status = %q", reply.Text)
	}
	if reply := svc.handlePremium(context.Background(), 1001, "/history"); !containsAll(reply.Text, "3 мес.", "подарок пользователю 2002") {
		t.Fatalf("/history = %q", reply.Text)
	}
	if !svc.HandlesBot(domain.PremiumBotConfiguredUserID()) {
		t.Fatal("Premium bot is not registered as a built-in responder")
	}
}

func TestPremiumBotUsesClientSessionLanguageForMessagesAndCallbacks(t *testing.T) {
	svc, users, _, messages := newTestService(t)
	owner := newOwner(t, users, "+15550888")
	svc.SetPremium(&fakePremiumStorefront{
		plans: []domain.PremiumPlan{premiumBotTestPlan()}, balance: 900,
	})
	locale := premiumLocaleFromSession(domain.ClientSessionMetadata{
		SessionID: 77, LangPack: "tdesktop", LangCode: "en-US",
	})
	home := svc.handlePremium(context.Background(), owner.ID, "/start", locale)
	if !strings.Contains(home.Text, "Welcome to the Premium store") ||
		home.ReplyMarkup == nil || home.ReplyMarkup.Inline[0][0].Text != "Buy Premium" {
		t.Fatalf("English Premium home = %+v", home)
	}

	_, handled, err := svc.OnCallbackQuery(context.Background(), domain.BotCallbackQuery{
		ID: 8, BotUserID: svc.premium.BotUserID(), UserID: owner.ID,
		Peer:      domain.Peer{Type: domain.PeerTypeUser, ID: owner.ID},
		MessageID: 1, Data: []byte(premiumCallbackPlans),
		ClientSession: domain.ClientSessionMetadata{SessionID: 77, LangCode: "en"},
	})
	if err != nil || !handled {
		t.Fatalf("English callback handled=%v err=%v", handled, err)
	}
	reply := latestPremiumBotReply(t, messages, owner.ID, svc.premium.BotUserID())
	if !strings.Contains(reply.Body, "Choose a Premium plan") {
		t.Fatalf("English Premium callback reply = %q", reply.Body)
	}
}

func TestPremiumBotMenuCallbacksExecuteInsideLocalServer(t *testing.T) {
	svc, users, _, messages := newTestService(t)
	owner := newOwner(t, users, "+15550999")
	svc.SetPremium(&fakePremiumStorefront{
		plans: []domain.PremiumPlan{premiumBotTestPlan()}, balance: 900,
	})

	home := svc.handlePremium(context.Background(), owner.ID, "/start")
	if home.ReplyMarkup == nil || len(home.ReplyMarkup.Inline) != 3 {
		t.Fatalf("home markup = %+v", home.ReplyMarkup)
	}
	for _, row := range home.ReplyMarkup.Inline {
		for _, button := range row {
			if button.Type != domain.MarkupButtonCallback || button.URL != "" ||
				!strings.HasPrefix(string(button.Data), premiumCallbackPrefix) {
				t.Fatalf("home button is not a local callback: %+v", button)
			}
		}
	}

	answer, handled, err := svc.OnCallbackQuery(context.Background(), domain.BotCallbackQuery{
		ID: 1, BotUserID: svc.premium.BotUserID(), UserID: owner.ID,
		Peer:      domain.Peer{Type: domain.PeerTypeUser, ID: owner.ID},
		MessageID: 1, Data: []byte(premiumCallbackPlans),
	})
	if err != nil || !handled || answer.Alert {
		t.Fatalf("plans callback answer = %+v handled=%v err=%v", answer, handled, err)
	}
	reply := latestPremiumBotReply(t, messages, owner.ID, svc.premium.BotUserID())
	if !strings.Contains(reply.Body, "Выберите тариф Premium") ||
		reply.ReplyMarkup == nil || len(reply.ReplyMarkup.Inline) != 1 ||
		string(reply.ReplyMarkup.Inline[0][0].Data) != premiumCallbackBuy+"3" {
		t.Fatalf("plans callback reply = %+v", reply)
	}

	_, handled, err = svc.OnCallbackQuery(context.Background(), domain.BotCallbackQuery{
		ID: 2, BotUserID: svc.premium.BotUserID(), UserID: owner.ID,
		Peer:      domain.Peer{Type: domain.PeerTypeUser, ID: owner.ID},
		MessageID: reply.ID, Data: []byte(premiumCallbackBuy + "3"),
	})
	if err != nil || !handled {
		t.Fatalf("buy callback handled=%v err=%v", handled, err)
	}
	invoice := latestPremiumBotReply(t, messages, owner.ID, svc.premium.BotUserID())
	if invoice.Media == nil || invoice.Media.Invoice == nil ||
		invoice.Media.Invoice.AmountStars != 750 {
		t.Fatalf("buy callback did not create catalog invoice: %+v", invoice)
	}
}

func latestPremiumBotReply(
	t *testing.T,
	messages *memory.MessageStore,
	userID, botID int64,
) domain.Message {
	t.Helper()
	list, err := messages.ListByUser(context.Background(), userID, domain.MessageFilter{
		HasPeer: true,
		Peer:    domain.Peer{Type: domain.PeerTypeUser, ID: botID},
		Limit:   100,
	})
	if err != nil {
		t.Fatalf("list premium bot replies: %v", err)
	}
	var latest domain.Message
	for _, message := range list.Messages {
		if message.From.ID == botID && message.ID > latest.ID {
			latest = message
		}
	}
	if latest.ID == 0 {
		t.Fatal("no Premium bot reply")
	}
	return latest
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}
