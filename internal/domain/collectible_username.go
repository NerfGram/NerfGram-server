package domain

import "errors"

// ErrCollectibleUsernameNotFound is returned when a username has no
// admin-assigned collectible metadata. This is the normal case for the vast
// majority of usernames -- it is not an error condition on its own, just a
// "nothing to show" signal for callers like fragment.getCollectibleInfo.
var ErrCollectibleUsernameNotFound = errors.New("collectible username not found")

// CollectibleUsername is admin-assigned metadata marking a username as a
// Fragment-style collectible/NFT username, served via
// fragment.getCollectibleInfo. Currency/Amount and CryptoCurrency/CryptoAmount
// are informational display fields set by an admin -- not a real payment
// rail, and Currency is free text (not restricted to ISO 4217), so a
// self-hosted server can use its own label (e.g. "YUT").
//
// A collectible username is issued as a brand-new, additional username for
// its owner (OwnerUserID) -- it never replaces or relabels an existing
// username. Active tracks whether it's the one currently shown/used for
// that account; the owner can switch via account.toggleUsername, same as
// real Telegram lets you switch between your editable username and any
// fragment-purchased one.
type CollectibleUsername struct {
	Username       string
	OwnerUserID    int64
	Active         bool
	PurchaseDate   int64
	Currency       string
	Amount         int64
	CryptoCurrency string
	CryptoAmount   int64
	URL            string
}

// UserVerification is an admin-issued "Verified by <org>" badge
// (tg.UserFull.bot_verification), distinct from the plain blue-checkmark
// Verified flag on the base User object. Real Telegram requires this to be
// set by an owned bot; here an admin assigns BotID/Icon/Description
// directly for a user.
type UserVerification struct {
	UserID      int64
	BotID       int64
	Icon        int64
	Description string
}
