package store

import (
	"context"

	"telesrv/internal/domain"
)

// PremiumStore owns the durable Premium catalog, forms, ledger-linked
// entitlements and their atomic purchase/refund transitions.
type PremiumStore interface {
	Plans(ctx context.Context) ([]domain.PremiumPlan, error)
	Plan(ctx context.Context, months int) (domain.PremiumPlan, bool, error)
	SyncPlans(ctx context.Context, plans []domain.PremiumPlan) error
	UpsertPremiumPlan(ctx context.Context, req domain.PremiumPlanUpsertRequest) (domain.PremiumPlan, error)
	IssuePremiumPaymentForm(ctx context.Context, form domain.PremiumPaymentForm) (domain.PremiumPaymentForm, error)
	PurchasePremium(ctx context.Context, req domain.PremiumPurchaseRequest) (domain.PremiumPurchaseResult, error)
	ActivePremiumEntitlements(ctx context.Context, userID int64, now int) ([]domain.PremiumEntitlement, error)
	PremiumEntitlements(ctx context.Context, userID int64, limit int) ([]domain.PremiumEntitlement, error)
	PremiumPurchaseHistory(ctx context.Context, userID int64, limit int) ([]domain.PremiumEntitlement, error)
	PremiumPayment(ctx context.Context, paymentIntentID int64) (domain.PremiumPaymentDetails, bool, error)
	SweepPremiumEntitlements(ctx context.Context, now, limit int) ([]domain.User, error)
	GrantPremiumEntitlement(ctx context.Context, req domain.PremiumAdminGrantRequest) (domain.PremiumEntitlement, domain.User, error)
	RevokePremiumEntitlements(ctx context.Context, req domain.PremiumAdminRevokeRequest) (domain.User, error)
	RefundPremiumPayment(ctx context.Context, req domain.PremiumRefundRequest) (domain.PremiumPurchaseResult, error)
}
