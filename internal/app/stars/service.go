// Package stars 实现 Stars 本地账本应用服务：余额查询、贷记/借记、流水分页，
// 以及「惰性首读授予」起始余额（靠 stars_balances.granted 布尔幂等，新老账号都覆盖、
// 无需回填迁移）。原子性由 store 事务保证；本层只做校验 + 授予策略。
package stars

import (
	"context"
	"time"

	"telesrv/internal/domain"
	"telesrv/internal/store"
)

// Service 是 Stars 账本应用服务。
type Service struct {
	store       store.StarsStore
	grantAmount int64
	now         func() time.Time
}

// Option 配置 Service。
type Option func(*Service)

// WithStartingGrant 设置惰性首读授予的起始余额；amount<=0 关闭自动授予。
func WithStartingGrant(amount int64) Option {
	return func(s *Service) { s.grantAmount = amount }
}

// WithClock 注入时钟（测试用）。
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// NewService 创建 Stars 账本服务，默认起始授予 domain.DefaultStarsStartingGrant。
func NewService(st store.StarsStore, opts ...Option) *Service {
	s := &Service{store: st, grantAmount: domain.DefaultStarsStartingGrant, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ensureGranted 惰性应用一次起始授予（幂等），返回最新余额。
func (s *Service) ensureGranted(ctx context.Context, userID int64) (domain.StarsBalance, error) {
	if s.grantAmount > 0 {
		bal, _, err := s.store.EnsureGrant(ctx, userID, s.grantAmount, int(s.now().Unix()))
		return bal, err
	}
	return s.store.GetBalance(ctx, userID)
}

// GetBalance 返回账号余额，首读时惰性授予起始余额。
func (s *Service) GetBalance(ctx context.Context, userID int64) (domain.StarsBalance, error) {
	return s.ensureGranted(ctx, userID)
}

// Credit 为账号入账（amount>0）。
func (s *Service) Credit(ctx context.Context, userID, amount int64, reason domain.StarsTransactionReason, peer domain.Peer, title, desc string) (domain.StarsBalance, error) {
	if amount <= 0 {
		return domain.StarsBalance{}, domain.ErrStarsInvalidAmount
	}
	return s.store.Credit(ctx, userID, amount, reason, peer, int(s.now().Unix()), title, desc)
}

// Debit 从账号扣款（amount>0），余额不足返回 domain.ErrStarsInsufficient。
// 先确保起始授予已应用，避免新账号在尚未首读余额前借记被误判余额不足。
func (s *Service) Debit(ctx context.Context, userID, amount int64, reason domain.StarsTransactionReason, peer domain.Peer, title, desc string) (domain.StarsBalance, error) {
	if amount <= 0 {
		return domain.StarsBalance{}, domain.ErrStarsInvalidAmount
	}
	if _, err := s.ensureGranted(ctx, userID); err != nil {
		return domain.StarsBalance{}, err
	}
	return s.store.Debit(ctx, userID, amount, reason, peer, int(s.now().Unix()), title, desc)
}

// ListTransactions 按 keyset 分页返回流水 + 当前余额，首读时惰性授予。
func (s *Service) ListTransactions(ctx context.Context, userID int64, offset string, limit int) (domain.StarsTransactionPage, error) {
	if len(offset) > domain.MaxStarsTransactionsOffsetBytes {
		offset = ""
	}
	if limit <= 0 || limit > domain.MaxStarsTransactionsLimit {
		limit = domain.MaxStarsTransactionsLimit
	}
	if _, err := s.ensureGranted(ctx, userID); err != nil {
		return domain.StarsTransactionPage{}, err
	}
	return s.store.ListTransactions(ctx, userID, offset, limit)
}

// TotalSpent returns cumulative Stars debited by the user.
func (s *Service) TotalSpent(ctx context.Context, userID int64) (int64, error) {
	return s.store.TotalSpent(ctx, userID)
}
