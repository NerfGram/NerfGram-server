package domain

import "fmt"

// DefaultStarsSubscriptionPeriod is Telegram's only allowed channel subscription
// billing period (30 days).
const DefaultStarsSubscriptionPeriod = 30 * 24 * 60 * 60

// StarsSubscription is one active/canceled channel Stars subscription for a user.
type StarsSubscription struct {
	ID         string
	UserID     int64
	ChannelID  int64
	InviteHash string
	UntilDate  int
	Period     int
	Amount     int64
	Canceled   bool
	Title      string
}

// NormalizeStarsSubscriptionPricing validates and normalizes invite pricing.
// Zero amount means a free invite (period cleared). Non-zero amount requires
// the official monthly period.
func NormalizeStarsSubscriptionPricing(period int, amount int64) (int, int64, error) {
	if amount < 0 || period < 0 {
		return 0, 0, fmt.Errorf("%w: period=%d amount=%d", ErrStarsInvalidAmount, period, amount)
	}
	if amount == 0 {
		return 0, 0, nil
	}
	if period == 0 {
		period = DefaultStarsSubscriptionPeriod
	}
	if period != DefaultStarsSubscriptionPeriod {
		return 0, 0, fmt.Errorf("%w: unsupported subscription period %d", ErrStarsInvalidAmount, period)
	}
	return period, amount, nil
}

// HasStarsSubscription reports whether the invite requires a Stars subscription.
func (i ChannelInvite) HasStarsSubscription() bool {
	return i.SubscriptionAmount > 0 && i.SubscriptionPeriod > 0
}
