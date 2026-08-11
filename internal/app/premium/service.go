// Package premium implements the application boundary for Telegram Premium
// purchases, gifts and durable entitlements.
package premium

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"telesrv/internal/domain"
	"telesrv/internal/store"
)

type Config struct {
	BotUserID int64
	Username  string
	Stars     StarsBalanceReader
}

// StarsBalanceReader keeps Premium on the existing Stars account lifecycle.
// In particular, GetBalance applies the configured one-time starting grant
// before a form can be paid; settlement still locks and debits the same rows
// atomically inside PremiumStore.
type StarsBalanceReader interface {
	GetBalance(ctx context.Context, userID int64) (domain.StarsBalance, error)
}

type Service struct {
	store     store.PremiumStore
	botUserID int64
	username  string
	stars     StarsBalanceReader
}

func NewService(st store.PremiumStore, cfg Config) *Service {
	if cfg.BotUserID <= 0 {
		cfg.BotUserID = domain.PremiumBotConfiguredUserID()
	}
	cfg.Username = strings.TrimPrefix(strings.TrimSpace(cfg.Username), "@")
	if cfg.Username == "" {
		cfg.Username = domain.PremiumBotUser().Username
	}
	return &Service{store: st, botUserID: cfg.BotUserID, username: cfg.Username, stars: cfg.Stars}
}

func (s *Service) BotUserID() int64 {
	if s == nil || s.botUserID <= 0 {
		return domain.PremiumBotConfiguredUserID()
	}
	return s.botUserID
}

func (s *Service) BotUsername() string {
	if s == nil || s.username == "" {
		return domain.PremiumBotUser().Username
	}
	return s.username
}

func (s *Service) Plans(ctx context.Context) ([]domain.PremiumPlan, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	plans, err := s.store.Plans(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.PremiumPlan, 0, len(plans))
	for _, plan := range plans {
		if plan.Enabled && plan.Valid() {
			out = append(out, plan)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].Months < out[j].Months
	})
	return out, nil
}

// Catalog returns enabled and disabled plans for the protected operator
// surface. Storefront callers continue to use Plans, which filters disabled
// rows.
func (s *Service) Catalog(ctx context.Context) ([]domain.PremiumPlan, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	plans, err := s.store.Plans(ctx)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(plans, func(i, j int) bool {
		if plans[i].SortOrder != plans[j].SortOrder {
			return plans[i].SortOrder < plans[j].SortOrder
		}
		return plans[i].Months < plans[j].Months
	})
	return plans, nil
}

func (s *Service) Plan(ctx context.Context, months int) (domain.PremiumPlan, error) {
	if s == nil || s.store == nil || months <= 0 {
		return domain.PremiumPlan{}, domain.ErrPremiumPlanUnavailable
	}
	plan, found, err := s.store.Plan(ctx, months)
	if err != nil {
		return domain.PremiumPlan{}, err
	}
	if !found || !plan.Enabled || !plan.Valid() {
		return domain.PremiumPlan{}, domain.ErrPremiumPlanUnavailable
	}
	return plan, nil
}

func (s *Service) SyncPlans(ctx context.Context, plans []domain.PremiumPlan) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("premium store is not configured")
	}
	if len(plans) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(plans))
	for _, plan := range plans {
		if !plan.Valid() {
			return domain.ErrPremiumPlanInvalid
		}
		if _, exists := seen[plan.Months]; exists {
			return domain.ErrPremiumPlanInvalid
		}
		seen[plan.Months] = struct{}{}
	}
	return s.store.SyncPlans(ctx, plans)
}

func (s *Service) UpsertPlan(
	ctx context.Context,
	req domain.PremiumPlanUpsertRequest,
) (domain.PremiumPlan, error) {
	if s == nil || s.store == nil {
		return domain.PremiumPlan{}, fmt.Errorf("premium store is not configured")
	}
	req.Label = strings.TrimSpace(req.Label)
	req.Reason = strings.TrimSpace(req.Reason)
	req.CommandKey = strings.TrimSpace(req.CommandKey)
	if !req.Valid() {
		return domain.PremiumPlan{}, domain.ErrPremiumPlanInvalid
	}
	return s.store.UpsertPremiumPlan(ctx, req)
}

func (s *Service) IssuePaymentForm(ctx context.Context, form domain.PremiumPaymentForm) (domain.PremiumPaymentForm, error) {
	if s == nil || s.store == nil || !form.Valid() {
		return domain.PremiumPaymentForm{}, domain.ErrPremiumFormInvalid
	}
	if s.stars != nil && form.EffectiveDebitStars() {
		if _, err := s.stars.GetBalance(ctx, form.BuyerUserID); err != nil {
			return domain.PremiumPaymentForm{}, err
		}
	}
	return s.store.IssuePremiumPaymentForm(ctx, form)
}

func (s *Service) Balance(ctx context.Context, userID int64) (domain.StarsBalance, error) {
	if s == nil || s.stars == nil || userID <= 0 {
		return domain.StarsBalance{}, domain.ErrStarsInvalidAmount
	}
	return s.stars.GetBalance(ctx, userID)
}

func (s *Service) Purchase(ctx context.Context, req domain.PremiumPurchaseRequest) (domain.PremiumPurchaseResult, error) {
	if s == nil || s.store == nil {
		return domain.PremiumPurchaseResult{}, domain.ErrPremiumPlanUnavailable
	}
	return s.store.PurchasePremium(ctx, req)
}

func (s *Service) ActiveEntitlements(ctx context.Context, userID int64, now int) ([]domain.PremiumEntitlement, error) {
	if s == nil || s.store == nil || userID <= 0 {
		return nil, nil
	}
	return s.store.ActivePremiumEntitlements(ctx, userID, now)
}

func (s *Service) Entitlements(ctx context.Context, userID int64, limit int) ([]domain.PremiumEntitlement, error) {
	if s == nil || s.store == nil || userID <= 0 {
		return nil, nil
	}
	return s.store.PremiumEntitlements(ctx, userID, limit)
}

func (s *Service) PurchaseHistory(ctx context.Context, userID int64, limit int) ([]domain.PremiumEntitlement, error) {
	if s == nil || s.store == nil || userID <= 0 {
		return nil, nil
	}
	return s.store.PremiumPurchaseHistory(ctx, userID, limit)
}

func (s *Service) Payment(ctx context.Context, paymentIntentID int64) (domain.PremiumPaymentDetails, bool, error) {
	if s == nil || s.store == nil || paymentIntentID <= 0 {
		return domain.PremiumPaymentDetails{}, false, nil
	}
	return s.store.PremiumPayment(ctx, paymentIntentID)
}

func (s *Service) SweepExpired(ctx context.Context, now, limit int) ([]domain.User, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	return s.store.SweepPremiumEntitlements(ctx, now, limit)
}

func (s *Service) Grant(ctx context.Context, req domain.PremiumAdminGrantRequest) (domain.PremiumEntitlement, domain.User, error) {
	if s == nil || s.store == nil {
		return domain.PremiumEntitlement{}, domain.User{}, domain.ErrPremiumPlanUnavailable
	}
	return s.store.GrantPremiumEntitlement(ctx, req)
}

func (s *Service) Revoke(ctx context.Context, req domain.PremiumAdminRevokeRequest) (domain.User, error) {
	if s == nil || s.store == nil {
		return domain.User{}, domain.ErrPremiumPlanUnavailable
	}
	return s.store.RevokePremiumEntitlements(ctx, req)
}

func (s *Service) Refund(ctx context.Context, req domain.PremiumRefundRequest) (domain.PremiumPurchaseResult, error) {
	if s == nil || s.store == nil {
		return domain.PremiumPurchaseResult{}, domain.ErrPremiumPaymentNotFound
	}
	return s.store.RefundPremiumPayment(ctx, req)
}
