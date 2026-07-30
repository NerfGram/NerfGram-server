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
type CollectibleUsername struct {
	Username       string
	PurchaseDate   int64
	Currency       string
	Amount         int64
	CryptoCurrency string
	CryptoAmount   int64
	URL            string
}
