package domain

import (
	"errors"
	"strings"
	"time"
)

const (
	MinCollectiblePhoneLength = 7
	MaxCollectiblePhoneLength = 15
)

var (
	ErrCollectiblePhoneInvalid    = errors.New("collectible phone invalid")
	ErrCollectiblePhoneNotFound   = errors.New("collectible phone not found")
	ErrCollectiblePhoneNotOwned   = errors.New("collectible phone not owned")
	ErrCollectiblePhoneBurned     = errors.New("collectible phone burned")
	ErrCollectiblePhoneOwnerLimit = errors.New("user already owns a collectible phone")
)

// CollectiblePhoneTier controls visibility. Standard numbers follow the
// account phone privacy rule; exclusive numbers are a server extension that
// remain visible on every projection of their owner.
type CollectiblePhoneTier string

const (
	CollectiblePhoneTierStandard  CollectiblePhoneTier = "standard"
	CollectiblePhoneTierExclusive CollectiblePhoneTier = "exclusive"
)

func (t CollectiblePhoneTier) Valid() bool {
	return t == CollectiblePhoneTierStandard || t == CollectiblePhoneTierExclusive
}

type CollectiblePhone struct {
	ID                  int64
	Phone               string
	Tier                CollectiblePhoneTier
	Status              CollectibleUsernameStatus
	OwnerUserID         int64
	PurchaseDate        time.Time
	Currency            string
	Amount              int64
	CryptoCurrency      string
	CryptoAmount        int64
	URL                 string
	OriginalOwnerUserID int64
	TransferCount       int
	Version             int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (c CollectiblePhone) Owned() bool {
	return c.Status == CollectibleUsernameStatusOwned && c.OwnerUserID > 0
}

func (c CollectiblePhone) AlwaysVisible() bool {
	return c.Owned() && c.Tier == CollectiblePhoneTierExclusive
}

func (c CollectiblePhone) Info() CollectibleInfo {
	date := 0
	if !c.PurchaseDate.IsZero() {
		date = int(c.PurchaseDate.Unix())
	}
	return CollectibleInfo{PurchaseDate: date, Currency: c.Currency, Amount: c.Amount,
		CryptoCurrency: c.CryptoCurrency, CryptoAmount: c.CryptoAmount, URL: c.URL}
}

func (c CollectiblePhone) Validate() error {
	if !ValidCollectiblePhone(c.Phone) || !c.Tier.Valid() || !c.Status.Valid() {
		return ErrCollectiblePhoneInvalid
	}
	if (c.Status == CollectibleUsernameStatusOwned) != (c.OwnerUserID > 0) {
		return ErrCollectiblePhoneInvalid
	}
	if err := ValidateCollectiblePhonePrice(c.Currency, c.Amount, c.CryptoCurrency, c.CryptoAmount); err != nil {
		return err
	}
	if len(c.URL) > MaxCollectibleUsernameURLLength || c.OriginalOwnerUserID < 0 || c.TransferCount < 0 {
		return ErrCollectiblePhoneInvalid
	}
	return nil
}

// ValidateCollectiblePhonePrice mirrors the shape Telegram Desktop expects in
// fragment.collectibleInfo: TON (nanotons) is drawn first and the USD purchase
// amount (cents) is drawn in parentheses. Leaving the crypto leg empty makes the
// client render the literal fallback "{:0}".
func ValidateCollectiblePhonePrice(currency string, amount int64, cryptoCurrency string, cryptoAmount int64) error {
	if currency != CollectibleCurrencyUSD || amount <= 0 || cryptoCurrency != CollectibleCryptoCurrencyTON || cryptoAmount <= 0 {
		return ErrCollectibleCurrencyInvalid
	}
	return nil
}

// NormalizeCollectiblePhone accepts the formatting clients commonly send and
// returns the digits stored on User.phone and inputCollectiblePhone.
func NormalizeCollectiblePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	var b strings.Builder
	for i, r := range phone {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0:
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '\t':
		default:
			return ""
		}
	}
	return b.String()
}

func ValidCollectiblePhone(phone string) bool {
	if len(phone) < MinCollectiblePhoneLength || len(phone) > MaxCollectiblePhoneLength || !strings.HasPrefix(phone, "888") {
		return false
	}
	for _, r := range phone {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

type MintCollectiblePhoneRequest struct {
	Phone          string
	Tier           CollectiblePhoneTier
	OwnerUserID    int64
	PurchaseDate   time.Time
	Currency       string
	Amount         int64
	CryptoCurrency string
	CryptoAmount   int64
	URL            string
	Actor          string
	Reason         string
	CommandKey     string
}

func (r MintCollectiblePhoneRequest) Validate() error {
	if !ValidCollectiblePhone(r.Phone) || !r.Tier.Valid() || r.OwnerUserID < 0 {
		return ErrCollectiblePhoneInvalid
	}
	if err := ValidateCollectiblePhonePrice(r.Currency, r.Amount, r.CryptoCurrency, r.CryptoAmount); err != nil {
		return err
	}
	if len(r.URL) > MaxCollectibleUsernameURLLength || len(r.Actor) > MaxCollectibleUsernameActorLength ||
		len(r.Reason) > MaxCollectibleUsernameReasonLength || len(r.CommandKey) > MaxCollectibleUsernameCommandKeyLength {
		return ErrCollectiblePhoneInvalid
	}
	return nil
}

type UpdateCollectiblePhonePriceRequest struct {
	Phone          string
	Currency       string
	Amount         int64
	CryptoCurrency string
	CryptoAmount   int64
	Actor          string
	Reason         string
}

func (r UpdateCollectiblePhonePriceRequest) Validate() error {
	if !ValidCollectiblePhone(r.Phone) || len(r.Actor) > MaxCollectibleUsernameActorLength || len(r.Reason) > MaxCollectibleUsernameReasonLength {
		return ErrCollectiblePhoneInvalid
	}
	return ValidateCollectiblePhonePrice(r.Currency, r.Amount, r.CryptoCurrency, r.CryptoAmount)
}

type TransferCollectiblePhoneRequest struct {
	Phone                     string
	ToUserID                  int64
	Actor, Reason, CommandKey string
}

func (r TransferCollectiblePhoneRequest) Validate() error {
	if !ValidCollectiblePhone(r.Phone) || r.ToUserID <= 0 || len(r.Actor) > MaxCollectibleUsernameActorLength || len(r.Reason) > MaxCollectibleUsernameReasonLength || len(r.CommandKey) > MaxCollectibleUsernameCommandKeyLength {
		return ErrCollectiblePhoneInvalid
	}
	return nil
}

type RevokeCollectiblePhoneRequest struct {
	Phone                     string
	Burn                      bool
	Actor, Reason, CommandKey string
}

func (r RevokeCollectiblePhoneRequest) Validate() error {
	if !ValidCollectiblePhone(r.Phone) || len(r.Actor) > MaxCollectibleUsernameActorLength || len(r.Reason) > MaxCollectibleUsernameReasonLength || len(r.CommandKey) > MaxCollectibleUsernameCommandKeyLength {
		return ErrCollectiblePhoneInvalid
	}
	return nil
}

type DeleteCollectiblePhoneRequest struct {
	Phone                     string
	Actor, Reason, CommandKey string
}

func (r DeleteCollectiblePhoneRequest) Validate() error {
	if !ValidCollectiblePhone(r.Phone) || len(r.Actor) > MaxCollectibleUsernameActorLength || len(r.Reason) > MaxCollectibleUsernameReasonLength || len(r.CommandKey) > MaxCollectibleUsernameCommandKeyLength {
		return ErrCollectiblePhoneInvalid
	}
	return nil
}

type CollectiblePhoneTransfer struct {
	ID, CollectibleID         int64
	Kind                      CollectibleUsernameTransferKind
	FromUserID, ToUserID      int64
	Currency                  string
	Amount                    int64
	Actor, Reason, CommandKey string
	CreatedAt                 time.Time
}

type CollectiblePhoneFilter struct {
	Status      CollectibleUsernameStatus
	Tier        CollectiblePhoneTier
	OwnerUserID int64
	Query       string
	BeforeID    int64
	Limit       int
}
