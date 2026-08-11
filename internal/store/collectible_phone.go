package store

import (
	"context"
	"telesrv/internal/domain"
)

type CollectiblePhoneStore interface {
	MintCollectiblePhone(context.Context, domain.MintCollectiblePhoneRequest) (domain.CollectiblePhone, bool, error)
	UpdateCollectiblePhonePrice(context.Context, domain.UpdateCollectiblePhonePriceRequest) (domain.CollectiblePhone, bool, error)
	TransferCollectiblePhone(context.Context, domain.TransferCollectiblePhoneRequest) (domain.CollectiblePhone, bool, error)
	RevokeCollectiblePhone(context.Context, domain.RevokeCollectiblePhoneRequest) (domain.CollectiblePhone, bool, error)
	DeleteCollectiblePhone(context.Context, domain.DeleteCollectiblePhoneRequest) (bool, error)
	CollectiblePhone(context.Context, string) (domain.CollectiblePhone, error)
	CollectiblePhoneByID(context.Context, int64) (domain.CollectiblePhone, error)
	OwnedCollectiblePhones(context.Context, []int64) (map[int64]domain.CollectiblePhone, error)
	ListCollectiblePhones(context.Context, domain.CollectiblePhoneFilter) ([]domain.CollectiblePhone, error)
	CollectiblePhoneTransfers(context.Context, int64, int) ([]domain.CollectiblePhoneTransfer, error)
}
