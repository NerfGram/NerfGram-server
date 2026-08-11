package rpc

import (
	"context"
	"testing"
	"time"

	"github.com/iamxvbaba/td/clock"
	"github.com/iamxvbaba/td/tg"
	"github.com/iamxvbaba/td/tgerr"
	"go.uber.org/zap/zaptest"
	"telesrv/internal/domain"
)

type collectiblePhoneRPCStore struct{ asset domain.CollectiblePhone }

func (s collectiblePhoneRPCStore) CollectiblePhone(_ context.Context, phone string) (domain.CollectiblePhone, error) {
	if phone != s.asset.Phone {
		return domain.CollectiblePhone{}, domain.ErrCollectiblePhoneNotFound
	}
	return s.asset, nil
}

type collectiblePhoneRPCUsers struct{ visible bool }

func (s collectiblePhoneRPCUsers) Self(context.Context, int64) (domain.User, error) {
	return domain.User{}, nil
}
func (s collectiblePhoneRPCUsers) ByID(_ context.Context, _, id int64) (domain.User, bool, error) {
	u := domain.User{ID: id}
	if s.visible {
		u.Phone = "8887777"
	}
	return u, true, nil
}
func (s collectiblePhoneRPCUsers) ByIDs(context.Context, int64, []int64) ([]domain.User, error) {
	return nil, nil
}

func TestFragmentCollectiblePhoneInfoHonorsProjectedVisibility(t *testing.T) {
	a := domain.CollectiblePhone{ID: 1, Phone: "8887777", Tier: domain.CollectiblePhoneTierExclusive, Status: domain.CollectibleUsernameStatusOwned, OwnerUserID: 42,
		PurchaseDate: time.Unix(100, 0), Currency: domain.CollectibleCurrencyUSD, Amount: 559300,
		CryptoCurrency: domain.CollectibleCryptoCurrencyTON, CryptoAmount: 3753000000000, URL: "https://fragment.com/number/8887777"}
	ctx := WithUserID(context.Background(), 7)
	r := New(Config{}, Deps{Users: collectiblePhoneRPCUsers{visible: true}, CollectiblePhones: collectiblePhoneRPCStore{asset: a}}, zaptest.NewLogger(t), clock.System)
	info, err := r.onFragmentGetCollectibleInfo(ctx, &tg.FragmentGetCollectibleInfoRequest{Collectible: &tg.InputCollectiblePhone{Phone: "+888 7777"}})
	if err != nil {
		t.Fatal(err)
	}
	if info.URL != a.URL || info.PurchaseDate != 100 || info.Currency != "USD" || info.Amount != 559300 ||
		info.CryptoCurrency != "TON" || info.CryptoAmount != 3753000000000 {
		t.Fatalf("info=%+v", info)
	}
	r = New(Config{}, Deps{Users: collectiblePhoneRPCUsers{}, CollectiblePhones: collectiblePhoneRPCStore{asset: a}}, zaptest.NewLogger(t), clock.System)
	if _, err := r.onFragmentGetCollectibleInfo(ctx, &tg.FragmentGetCollectibleInfoRequest{Collectible: &tg.InputCollectiblePhone{Phone: a.Phone}}); !tgerr.Is(err, "COLLECTIBLE_NOT_FOUND") {
		t.Fatalf("hidden err=%v", err)
	}
}
