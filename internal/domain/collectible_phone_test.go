package domain

import "testing"

func TestCollectiblePhoneNormalizationAndClasses(t *testing.T) {
	for input, want := range map[string]string{
		"+888 11-11":     "8881111",
		"(888) 123 4567": "8881234567",
	} {
		if got := NormalizeCollectiblePhone(input); got != want || !ValidCollectiblePhone(got) {
			t.Fatalf("NormalizeCollectiblePhone(%q) = %q, want valid %q", input, got, want)
		}
	}
	for _, invalid := range []string{"+7771111", "+888abc1", "+8881", "+8881234567890123"} {
		if got := NormalizeCollectiblePhone(invalid); got != "" && ValidCollectiblePhone(got) {
			t.Fatalf("%q normalized to valid collectible %q", invalid, got)
		}
	}
	standard := CollectiblePhone{Phone: "8881111", Tier: CollectiblePhoneTierStandard, Status: CollectibleUsernameStatusOwned, OwnerUserID: 1}
	exclusive := standard
	exclusive.Tier = CollectiblePhoneTierExclusive
	if standard.AlwaysVisible() || !exclusive.AlwaysVisible() {
		t.Fatalf("tier visibility mismatch: standard=%v exclusive=%v", standard.AlwaysVisible(), exclusive.AlwaysVisible())
	}
}

func TestCollectiblePhonePriceMatchesDesktopCollectibleInfo(t *testing.T) {
	if err := ValidateCollectiblePhonePrice("USD", 559300, "TON", 3753000000000); err != nil {
		t.Fatalf("valid Fragment price rejected: %v", err)
	}
	for _, invalid := range []struct {
		currency       string
		amount         int64
		cryptoCurrency string
		cryptoAmount   int64
	}{
		{currency: "TON", amount: 50},
		{currency: "USD", amount: 559300},
		{currency: "USD", amount: 559300, cryptoCurrency: "TON"},
		{currency: "USD", amount: 0, cryptoCurrency: "TON", cryptoAmount: 1},
	} {
		if err := ValidateCollectiblePhonePrice(invalid.currency, invalid.amount, invalid.cryptoCurrency, invalid.cryptoAmount); err == nil {
			t.Fatalf("invalid Fragment price accepted: %+v", invalid)
		}
	}
}
