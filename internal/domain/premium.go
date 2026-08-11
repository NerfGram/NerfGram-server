package domain

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// PremiumPromoCatalog is the immutable, domain-only media catalog returned by
// help.getPremiumPromo. VideoSections[i] describes Videos[i]; callers must
// preserve the one-to-one ordering because official clients use positional
// lookup.
type PremiumPromoCatalog struct {
	VideoSections []string
	Videos        []Document
}

const (
	PremiumCurrencyStars          = "XTR"
	PremiumDefaultFiatCurrency    = "USD"
	PremiumPaymentFormTTLSeconds  = 10 * 60
	MaxPremiumGiftMessageRunes    = 128
	MaxPremiumGiftMessageEntities = 100
	MaxPremiumPlanMonths          = 120
	MaxPremiumPlanDurationDays    = 36500
	MaxPremiumPlanAmountStars     = int64(1_000_000_000_000_000)
	MaxPremiumUnixTimestamp       = 1<<31 - 1
)

type PremiumPurchaseKind string

const (
	PremiumPurchaseSelf PremiumPurchaseKind = "self"
	PremiumPurchaseGift PremiumPurchaseKind = "gift"
)

type PremiumEntitlementSource string

const (
	PremiumEntitlementPurchase PremiumEntitlementSource = "purchase"
	PremiumEntitlementGift     PremiumEntitlementSource = "gift"
	PremiumEntitlementGiftCode PremiumEntitlementSource = "gift_code"
	PremiumEntitlementAdmin    PremiumEntitlementSource = "admin"
	PremiumEntitlementPromo    PremiumEntitlementSource = "promo"
)

type PremiumEntitlementStatus string

const (
	PremiumEntitlementActive   PremiumEntitlementStatus = "active"
	PremiumEntitlementExpired  PremiumEntitlementStatus = "expired"
	PremiumEntitlementRevoked  PremiumEntitlementStatus = "revoked"
	PremiumEntitlementRefunded PremiumEntitlementStatus = "refunded"
)

type PremiumPaymentStatus string

const (
	PremiumPaymentPending    PremiumPaymentStatus = "pending"
	PremiumPaymentProcessing PremiumPaymentStatus = "processing"
	PremiumPaymentPaid       PremiumPaymentStatus = "paid"
	PremiumPaymentFailed     PremiumPaymentStatus = "failed"
	PremiumPaymentRefunded   PremiumPaymentStatus = "refunded"
	PremiumPaymentExpired    PremiumPaymentStatus = "expired"
)

// PremiumPlan is the authoritative server-side price and duration snapshot.
// Version is copied into every payment intent, so a catalog edit can never
// change the amount or duration of an already issued form.
type PremiumPlan struct {
	Months        int
	DurationDays  int
	AmountStars   int64
	FiatCurrency  string
	FiatAmount    int64
	StoreProduct  string
	StoreQuantity int
	Enabled       bool
	SortOrder     int
	Label         string
	ManagedBy     string
	Version       int64
	UpdatedAt     int
}

func (p PremiumPlan) Valid() bool {
	currency := p.EffectiveFiatCurrency()
	fiatAmount := p.EffectiveFiatAmount()
	return p.Months > 0 && p.Months <= MaxPremiumPlanMonths &&
		p.DurationDays > 0 && p.DurationDays <= MaxPremiumPlanDurationDays &&
		p.AmountStars > 0 && p.AmountStars <= MaxPremiumPlanAmountStars &&
		p.Version > 0 && p.Label != "" && validPremiumCurrency(currency) &&
		fiatAmount > 0 && fiatAmount <= MaxPremiumPlanAmountStars &&
		len(p.StoreProduct) <= 256 && p.StoreQuantity >= 0 &&
		((p.StoreProduct == "" && p.StoreQuantity == 0) ||
			(p.StoreProduct != "" && p.StoreQuantity > 0))
}

func (p PremiumPlan) EffectiveFiatCurrency() string {
	currency := strings.ToUpper(strings.TrimSpace(p.FiatCurrency))
	if currency == "" {
		return PremiumDefaultFiatCurrency
	}
	return currency
}

func (p PremiumPlan) EffectiveFiatAmount() int64 {
	if p.FiatAmount <= 0 {
		return p.AmountStars
	}
	return p.FiatAmount
}

func validPremiumCurrency(currency string) bool {
	if len(currency) != 3 {
		return false
	}
	for _, r := range currency {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

const (
	PremiumPlanManagedByConfig = "config"
	PremiumPlanManagedByAdmin  = "admin"
)

// PremiumPlanUpsertRequest is the operator-facing optimistic write model.
// ExpectedVersion=0 creates a new month option; a positive value updates
// exactly that revision. Plans are disabled instead of deleted because issued
// payment forms retain their catalog snapshot and version.
type PremiumPlanUpsertRequest struct {
	Months          int
	DurationDays    int
	AmountStars     int64
	FiatCurrency    string
	FiatAmount      int64
	StoreProduct    string
	StoreQuantity   int
	Enabled         bool
	SortOrder       int
	Label           string
	ExpectedVersion int64
	ActorUserID     int64
	Date            int
	Reason          string
	CommandKey      string
}

func (r PremiumPlanUpsertRequest) Valid() bool {
	return PremiumPlan{
		Months:        r.Months,
		DurationDays:  r.DurationDays,
		AmountStars:   r.AmountStars,
		FiatCurrency:  r.FiatCurrency,
		FiatAmount:    r.FiatAmount,
		StoreProduct:  r.StoreProduct,
		StoreQuantity: r.StoreQuantity,
		Enabled:       r.Enabled,
		SortOrder:     r.SortOrder,
		Label:         r.Label,
		Version:       1,
	}.Valid() &&
		r.ExpectedVersion >= 0 && r.ActorUserID > 0 && r.Date > 0 &&
		r.CommandKey != "" && len(r.CommandKey) <= 256 && len(r.Reason) <= 1024
}

type PremiumGiftMessage struct {
	Text     string          `json:"text"`
	Entities []MessageEntity `json:"entities,omitempty"`
}

func (m PremiumGiftMessage) Valid() bool {
	if !utf8.ValidString(m.Text) ||
		len([]rune(m.Text)) > MaxPremiumGiftMessageRunes ||
		len(m.Entities) > MaxPremiumGiftMessageEntities {
		return false
	}
	encoded := utf16.Encode([]rune(m.Text))
	units := len(encoded)
	boundaries := make([]bool, units+1)
	boundaries[0] = true
	position := 0
	for _, r := range m.Text {
		position += len(utf16.Encode([]rune{r}))
		boundaries[position] = true
	}
	for _, entity := range m.Entities {
		if !PremiumGiftEntityTypeAllowed(entity.Type) {
			return false
		}
		if entity.Offset < 0 || entity.Length <= 0 ||
			entity.Offset > units || entity.Length > units-entity.Offset ||
			!boundaries[entity.Offset] || !boundaries[entity.Offset+entity.Length] {
			return false
		}
		if entity.Type == MessageEntityCustomEmoji && entity.DocumentID <= 0 {
			return false
		}
	}
	return true
}

// PremiumGiftEntityTypeAllowed mirrors the formatted-text subset accepted by
// Telegram's giftPremiumSubscription surface. Keeping it in the domain model
// makes MTProto, Bot API and persisted invoice validation identical.
func PremiumGiftEntityTypeAllowed(entityType MessageEntityType) bool {
	switch entityType {
	case MessageEntityBold, MessageEntityItalic, MessageEntityUnderline,
		MessageEntityStrike, MessageEntitySpoiler, MessageEntityCustomEmoji:
		return true
	default:
		return false
	}
}

// PremiumInvoice is persisted as message media for invoices sent by
// @premiumbot. It contains no client-controlled price.
type PremiumInvoice struct {
	Kind            PremiumPurchaseKind `json:"kind"`
	RecipientUserID int64               `json:"recipient_user_id,omitempty"`
	Months          int                 `json:"months"`
	DurationDays    int                 `json:"duration_days"`
	AmountStars     int64               `json:"amount_stars"`
	PaymentCurrency string              `json:"payment_currency,omitempty"`
	PaymentAmount   int64               `json:"payment_amount,omitempty"`
	DebitStars      bool                `json:"debit_stars,omitempty"`
	PlanVersion     int64               `json:"plan_version"`
	Title           string              `json:"title"`
	Description     string              `json:"description"`
	StartParam      string              `json:"start_param,omitempty"`
	Message         PremiumGiftMessage  `json:"message,omitempty"`
}

func (i PremiumInvoice) Valid() bool {
	if i.Kind != PremiumPurchaseSelf && i.Kind != PremiumPurchaseGift {
		return false
	}
	if i.Kind == PremiumPurchaseSelf && i.RecipientUserID != 0 {
		return false
	}
	if i.Kind == PremiumPurchaseGift && i.RecipientUserID <= 0 {
		return false
	}
	return i.Months > 0 && i.Months <= MaxPremiumPlanMonths &&
		i.DurationDays > 0 && i.DurationDays <= MaxPremiumPlanDurationDays &&
		i.AmountStars > 0 && i.AmountStars <= MaxPremiumPlanAmountStars &&
		i.PlanVersion > 0 && i.Title != "" && i.Description != "" &&
		validPremiumCurrency(i.EffectivePaymentCurrency()) &&
		i.EffectivePaymentAmount() > 0 &&
		i.EffectivePaymentAmount() <= MaxPremiumPlanAmountStars &&
		i.Message.Valid()
}

func (i PremiumInvoice) EffectivePaymentCurrency() string {
	if currency := strings.ToUpper(strings.TrimSpace(i.PaymentCurrency)); currency != "" {
		return currency
	}
	return PremiumCurrencyStars
}

func (i PremiumInvoice) EffectivePaymentAmount() int64 {
	if i.PaymentAmount > 0 {
		return i.PaymentAmount
	}
	return i.AmountStars
}

func (i PremiumInvoice) EffectiveDebitStars() bool {
	return i.PaymentCurrency == "" || i.EffectivePaymentCurrency() == PremiumCurrencyStars || i.DebitStars
}

type PremiumPaymentForm struct {
	ID              int64
	IdempotencyKey  string
	BuyerUserID     int64
	Kind            PremiumPurchaseKind
	RecipientUserID int64
	Months          int
	DurationDays    int
	AmountStars     int64
	PaymentCurrency string
	PaymentAmount   int64
	DebitStars      bool
	PlanVersion     int64
	Message         PremiumGiftMessage
	IssuedAt        int
	ExpiresAt       int
}

func (f PremiumPaymentForm) Valid() bool {
	if f.ID != 0 || f.BuyerUserID <= 0 ||
		len(f.IdempotencyKey) > 256 ||
		f.IssuedAt > MaxPremiumUnixTimestamp-PremiumPaymentFormTTLSeconds ||
		f.IssuedAt <= 0 || f.ExpiresAt != f.IssuedAt+PremiumPaymentFormTTLSeconds ||
		f.Months <= 0 || f.Months > MaxPremiumPlanMonths ||
		f.DurationDays <= 0 || f.DurationDays > MaxPremiumPlanDurationDays ||
		f.AmountStars <= 0 || f.AmountStars > MaxPremiumPlanAmountStars ||
		f.PlanVersion <= 0 || !validPremiumCurrency(f.EffectivePaymentCurrency()) ||
		f.EffectivePaymentAmount() <= 0 ||
		f.EffectivePaymentAmount() > MaxPremiumPlanAmountStars || !f.Message.Valid() {
		return false
	}
	switch f.Kind {
	case PremiumPurchaseSelf:
		return f.RecipientUserID == f.BuyerUserID
	case PremiumPurchaseGift:
		return f.RecipientUserID > 0 && f.RecipientUserID != f.BuyerUserID
	default:
		return false
	}
}

func (f PremiumPaymentForm) EffectivePaymentCurrency() string {
	if currency := strings.ToUpper(strings.TrimSpace(f.PaymentCurrency)); currency != "" {
		return currency
	}
	return PremiumCurrencyStars
}

func (f PremiumPaymentForm) EffectivePaymentAmount() int64 {
	if f.PaymentAmount > 0 {
		return f.PaymentAmount
	}
	return f.AmountStars
}

func (f PremiumPaymentForm) EffectiveDebitStars() bool {
	return f.PaymentCurrency == "" || f.EffectivePaymentCurrency() == PremiumCurrencyStars || f.DebitStars
}

type PremiumPurchaseRequest struct {
	BuyerUserID      int64
	FormID           int64
	Kind             PremiumPurchaseKind
	RecipientUserID  int64
	Months           int
	PlanVersion      int64
	Message          PremiumGiftMessage
	Date             int
	CommandKey       string
	OriginAuthKeyID  [8]byte
	OriginSessionID  int64
	RecipientBlocked bool
}

type PremiumEntitlement struct {
	ID              int64                    `json:"id"`
	UserID          int64                    `json:"user_id"`
	Source          PremiumEntitlementSource `json:"source"`
	SourceUserID    int64                    `json:"source_user_id"`
	PaymentIntentID int64                    `json:"payment_intent_id,omitempty"`
	TransactionID   int64                    `json:"transaction_id,omitempty"`
	Months          int                      `json:"months"`
	DurationDays    int                      `json:"duration_days"`
	StartsAt        int                      `json:"starts_at"`
	ExpiresAt       int                      `json:"expires_at"`
	Status          PremiumEntitlementStatus `json:"status"`
	CommandKey      string                   `json:"command_key,omitempty"`
	CreatedAt       int                      `json:"created_at"`
}

// PremiumPaymentIntent is the immutable checkout snapshot plus lifecycle
// pointers used by the protected operator read surface.
type PremiumPaymentIntent struct {
	ID                 int64                `json:"id"`
	FormID             int64                `json:"form_id"`
	IdempotencyKey     string               `json:"idempotency_key"`
	BuyerUserID        int64                `json:"buyer_user_id"`
	Kind               PremiumPurchaseKind  `json:"kind"`
	RecipientUserID    int64                `json:"recipient_user_id"`
	Months             int                  `json:"months"`
	DurationDays       int                  `json:"duration_days"`
	AmountStars        int64                `json:"amount_stars"`
	Currency           string               `json:"currency"`
	PaymentAmount      int64                `json:"payment_amount"`
	DebitStars         bool                 `json:"debit_stars"`
	PlanVersion        int64                `json:"plan_version"`
	Message            PremiumGiftMessage   `json:"message"`
	Status             PremiumPaymentStatus `json:"status"`
	IssuedAt           int                  `json:"issued_at"`
	ExpiresAt          int                  `json:"expires_at"`
	PaidAt             int                  `json:"paid_at,omitempty"`
	RefundedAt         int                  `json:"refunded_at,omitempty"`
	StarsTransactionID int64                `json:"stars_transaction_id,omitempty"`
	SenderMessageID    int                  `json:"sender_message_id,omitempty"`
	RecipientMessageID int                  `json:"recipient_message_id,omitempty"`
	CreatedAt          int                  `json:"created_at"`
	UpdatedAt          int                  `json:"updated_at"`
}

type PremiumPaymentDetails struct {
	Intent      PremiumPaymentIntent `json:"payment_intent"`
	Entitlement PremiumEntitlement   `json:"entitlement"`
	Transaction StarsTransaction     `json:"ledger_transaction"`
}

type PremiumPurchaseResult struct {
	Form        PremiumPaymentForm
	Entitlement PremiumEntitlement
	User        User
	Balance     StarsBalance
	Send        SendPrivateTextResult
	Duplicate   bool
}

type PremiumAdminGrantRequest struct {
	UserID       int64
	ActorUserID  int64
	Months       int
	DurationDays int
	Date         int
	Reason       string
	CommandKey   string
}

type PremiumAdminRevokeRequest struct {
	UserID        int64
	ActorUserID   int64
	EntitlementID int64
	Date          int
	Reason        string
	CommandKey    string
}

type PremiumRefundRequest struct {
	PaymentIntentID int64
	ActorUserID     int64
	Date            int
	Reason          string
	CommandKey      string
}

// PremiumSubscriptionActiveError prevents the single-recipient Premium gift
// flow from silently stacking another entitlement on an already active one.
// Until is the recipient's current expiry as a Unix timestamp.
type PremiumSubscriptionActiveError struct {
	Until int
}

func (e PremiumSubscriptionActiveError) Error() string {
	return fmt.Sprintf("premium: subscription active until %d", e.Until)
}

var (
	ErrPremiumPlanInvalid         = errors.New("premium: invalid plan")
	ErrPremiumPlanUnavailable     = errors.New("premium: plan unavailable")
	ErrPremiumPlanConflict        = errors.New("premium: plan version conflict")
	ErrPremiumLastPlan            = errors.New("premium: at least one enabled plan is required")
	ErrPremiumRecipientInvalid    = errors.New("premium: invalid recipient")
	ErrPremiumRecipientRestricted = errors.New("premium: recipient disallows premium gifts")
	ErrPremiumGiftSelf            = errors.New("premium: gifting self is not allowed")
	ErrPremiumGiftMessageInvalid  = errors.New("premium: invalid gift message")
	ErrPremiumFormExpired         = errors.New("premium: payment form expired")
	ErrPremiumFormInvalid         = errors.New("premium: invalid payment form")
	ErrPremiumFormAmountChanged   = errors.New("premium: payment form amount changed")
	ErrPremiumInvoiceAlreadyPaid  = errors.New("premium: invoice already paid")
	ErrPremiumPaymentNotFound     = errors.New("premium: payment not found")
	ErrPremiumAlreadyRefunded     = errors.New("premium: payment already refunded")
	ErrPremiumExternalRefund      = errors.New("premium: external payment must be refunded by its provider")
)

func PremiumPurchaseDescription(kind PremiumPurchaseKind, months int, recipientID int64) string {
	if kind == PremiumPurchaseGift {
		return fmt.Sprintf("Premium gift: %d month(s) for user %d", months, recipientID)
	}
	return fmt.Sprintf("Premium subscription: %d month(s)", months)
}
