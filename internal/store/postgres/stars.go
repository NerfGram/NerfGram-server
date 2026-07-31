package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"telesrv/internal/domain"
	"telesrv/internal/store/postgres/sqlcgen"
)

// StarsStore 用 PostgreSQL 实现 store.StarsStore（Stars 本地账本）。
// 借记/贷记/授予各自在单事务内完成：余额与流水永不漂移。
type StarsStore struct {
	db sqlcgen.DBTX
}

// NewStarsStore 基于 pgx 连接池（或事务）创建 StarsStore。
func NewStarsStore(db sqlcgen.DBTX) *StarsStore {
	return &StarsStore{db: db}
}

func (s *StarsStore) GetBalance(ctx context.Context, userID int64) (domain.StarsBalance, error) {
	if userID == 0 {
		return domain.StarsBalance{}, nil
	}
	bal := domain.StarsBalance{UserID: userID}
	err := s.db.QueryRow(ctx, `SELECT balance, granted FROM stars_balances WHERE user_id = $1`, userID).
		Scan(&bal.Balance, &bal.Granted)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.StarsBalance{UserID: userID}, nil
		}
		return domain.StarsBalance{}, fmt.Errorf("get stars balance: %w", err)
	}
	return bal, nil
}

func (s *StarsStore) EnsureGrant(ctx context.Context, userID, amount int64, date int) (domain.StarsBalance, bool, error) {
	if userID == 0 {
		return domain.StarsBalance{}, false, nil
	}
	if amount <= 0 {
		// 授予额为 0 时只确保有一行且 granted=true（幂等关闭授予）。
		bal, err := s.GetBalance(ctx, userID)
		return bal, false, err
	}
	out := domain.StarsBalance{UserID: userID}
	applied := false
	err := withTx(ctx, s.db, "ensure stars grant", func(tx pgx.Tx) error {
		var balance int64
		var granted bool
		err := tx.QueryRow(ctx, `SELECT balance, granted FROM stars_balances WHERE user_id = $1 FOR UPDATE`, userID).
			Scan(&balance, &granted)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if _, err := tx.Exec(ctx, `INSERT INTO stars_balances (user_id, balance, granted, updated_at) VALUES ($1, $2, true, now())`,
				userID, amount); err != nil {
				return fmt.Errorf("insert stars balance grant: %w", err)
			}
			out.Balance, out.Granted, applied = amount, true, true
		case err != nil:
			return fmt.Errorf("select stars balance for grant: %w", err)
		case granted:
			out.Balance, out.Granted = balance, true
			return nil
		default:
			if err := tx.QueryRow(ctx, `UPDATE stars_balances SET balance = balance + $2, granted = true, updated_at = now() WHERE user_id = $1 RETURNING balance`,
				userID, amount).Scan(&out.Balance); err != nil {
				return fmt.Errorf("update stars balance grant: %w", err)
			}
			out.Granted, applied = true, true
		}
		return insertStarsTxn(ctx, tx, userID, amount, domain.StarsReasonGrant, domain.Peer{}, date, "", "")
	})
	if err != nil {
		return domain.StarsBalance{}, false, err
	}
	return out, applied, nil
}

func (s *StarsStore) Credit(ctx context.Context, userID, amount int64, reason domain.StarsTransactionReason, peer domain.Peer, date int, title, desc string) (domain.StarsBalance, error) {
	if userID == 0 || amount <= 0 {
		return domain.StarsBalance{}, domain.ErrStarsInvalidAmount
	}
	out := domain.StarsBalance{UserID: userID}
	err := withTx(ctx, s.db, "credit stars", func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
INSERT INTO stars_balances (user_id, balance, updated_at) VALUES ($1, $2, now())
ON CONFLICT (user_id) DO UPDATE SET balance = stars_balances.balance + EXCLUDED.balance, updated_at = now()
RETURNING balance, granted`, userID, amount).Scan(&out.Balance, &out.Granted); err != nil {
			return fmt.Errorf("credit stars balance: %w", err)
		}
		return insertStarsTxn(ctx, tx, userID, amount, reason, peer, date, title, desc)
	})
	if err != nil {
		return domain.StarsBalance{}, err
	}
	return out, nil
}

func (s *StarsStore) Debit(ctx context.Context, userID, amount int64, reason domain.StarsTransactionReason, peer domain.Peer, date int, title, desc string) (domain.StarsBalance, error) {
	if userID == 0 || amount <= 0 {
		return domain.StarsBalance{}, domain.ErrStarsInvalidAmount
	}
	out := domain.StarsBalance{UserID: userID, Granted: true}
	err := withTx(ctx, s.db, "debit stars", func(tx pgx.Tx) error {
		var balance int64
		var granted bool
		err := tx.QueryRow(ctx, `SELECT balance, granted FROM stars_balances WHERE user_id = $1 FOR UPDATE`, userID).
			Scan(&balance, &granted)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && balance < amount) {
			return domain.ErrStarsInsufficient
		}
		if err != nil {
			return fmt.Errorf("select stars balance for debit: %w", err)
		}
		if err := tx.QueryRow(ctx, `UPDATE stars_balances SET balance = balance - $2, updated_at = now() WHERE user_id = $1 RETURNING balance`,
			userID, amount).Scan(&out.Balance); err != nil {
			return fmt.Errorf("update stars balance debit: %w", err)
		}
		out.Granted = granted
		return insertStarsTxn(ctx, tx, userID, -amount, reason, peer, date, title, desc)
	})
	if err != nil {
		return domain.StarsBalance{}, err
	}
	return out, nil
}

func (s *StarsStore) ListTransactions(ctx context.Context, userID int64, offset string, limit int) (domain.StarsTransactionPage, error) {
	if userID == 0 {
		return domain.StarsTransactionPage{}, nil
	}
	if limit <= 0 || limit > domain.MaxStarsTransactionsLimit {
		limit = domain.MaxStarsTransactionsLimit
	}
	bal, err := s.GetBalance(ctx, userID)
	if err != nil {
		return domain.StarsTransactionPage{}, err
	}
	page := domain.StarsTransactionPage{Balance: bal.Balance}

	// keyset：多取一条以探测是否还有下一页。
	args := []any{userID, limit + 1}
	query := `
SELECT id, peer_type, peer_id, amount, reason, title, description, date
FROM stars_transactions
WHERE user_id = $1
ORDER BY id DESC
LIMIT $2`
	if cursor, ok := domain.DecodeStarsCursor(offset); ok {
		query = `
SELECT id, peer_type, peer_id, amount, reason, title, description, date
FROM stars_transactions
WHERE user_id = $1 AND id < $3
ORDER BY id DESC
LIMIT $2`
		args = append(args, cursor)
	}
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return domain.StarsTransactionPage{}, fmt.Errorf("list stars transactions: %w", err)
	}
	defer rows.Close()
	txns := make([]domain.StarsTransaction, 0, limit)
	for rows.Next() {
		var (
			t        domain.StarsTransaction
			peerType string
			peerID   int64
			reason   string
		)
		if err := rows.Scan(&t.ID, &peerType, &peerID, &t.Amount, &reason, &t.Title, &t.Description, &t.Date); err != nil {
			return domain.StarsTransactionPage{}, fmt.Errorf("scan stars transaction: %w", err)
		}
		t.UserID = userID
		t.Reason = domain.StarsTransactionReason(reason)
		if peerType != "" {
			t.Peer = domain.Peer{Type: domain.PeerType(peerType), ID: peerID}
		}
		txns = append(txns, t)
	}
	if err := rows.Err(); err != nil {
		return domain.StarsTransactionPage{}, fmt.Errorf("iterate stars transactions: %w", err)
	}
	if len(txns) > limit {
		txns = txns[:limit]
		page.NextOffset = domain.EncodeStarsCursor(txns[len(txns)-1].ID)
	}
	page.Transactions = txns
	return page, nil
}

func (s *StarsStore) TotalSpent(ctx context.Context, userID int64) (int64, error) {
	if userID == 0 {
		return 0, nil
	}
	var spent int64
	err := s.db.QueryRow(ctx, `
SELECT COALESCE(SUM(-amount), 0)
FROM stars_transactions
WHERE user_id = $1 AND amount < 0`, userID).Scan(&spent)
	if err != nil {
		return 0, fmt.Errorf("sum stars spent: %w", err)
	}
	return spent, nil
}

// insertStarsTxn 在事务内写一条流水（amount 带符号）。
func insertStarsTxn(ctx context.Context, tx pgx.Tx, userID, amount int64, reason domain.StarsTransactionReason, peer domain.Peer, date int, title, desc string) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO stars_transactions (user_id, peer_type, peer_id, amount, reason, title, description, date)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		userID, string(peer.Type), peer.ID, amount, string(reason), title, desc, date); err != nil {
		return fmt.Errorf("insert stars transaction: %w", err)
	}
	return nil
}
