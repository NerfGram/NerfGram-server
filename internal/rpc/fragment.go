package rpc

import (
	"context"

	"github.com/iamxvbaba/td/tg"
	"github.com/iamxvbaba/td/tgerr"
	"github.com/iamxvbaba/td/tlprofile"
)

func collectibleInfoInvalidErr() error { return tgerr.New(400, "COLLECTIBLE_INFO_INVALID") }

func (r *Router) registerFragment(d *tlprofile.Dispatcher) {
	registerRPC[*tg.FragmentGetCollectibleInfoRequest](d, tlprofile.SemanticMethodFragmentGetCollectibleInfo, func(ctx context.Context, req *tg.FragmentGetCollectibleInfoRequest) (any, error) {
		return r.onFragmentGetCollectibleInfo(ctx, req)
	})
}

// onFragmentGetCollectibleInfo serves admin-assigned collectible/NFT-style
// metadata for a username. Only usernames an admin has explicitly marked as
// collectible have a row; everything else returns collectibleInfoInvalidErr,
// which is the normal, expected outcome for ordinary usernames -- the client
// simply doesn't show the collectible badge/bottom sheet in that case.
func (r *Router) onFragmentGetCollectibleInfo(ctx context.Context, req *tg.FragmentGetCollectibleInfoRequest) (*tg.FragmentCollectibleInfo, error) {
	if r.deps.CollectibleUsernames == nil {
		return nil, collectibleInfoInvalidErr()
	}
	usernameInput, ok := req.Collectible.(*tg.InputCollectibleUsername)
	if !ok {
		// Collectible phone numbers aren't implemented; treat like "not found"
		// rather than exposing an internal distinction to the client.
		return nil, collectibleInfoInvalidErr()
	}
	cu, found, err := r.deps.CollectibleUsernames.Get(ctx, usernameInput.Username)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, collectibleInfoInvalidErr()
	}
	return &tg.FragmentCollectibleInfo{
		PurchaseDate:   int(cu.PurchaseDate),
		Currency:       cu.Currency,
		Amount:         cu.Amount,
		CryptoCurrency: cu.CryptoCurrency,
		CryptoAmount:   cu.CryptoAmount,
		URL:            cu.URL,
	}, nil
}
