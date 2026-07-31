package memory

import (
	"context"
	"sync"

	"telesrv/internal/domain"
)

// StarsStore 是 store.StarsStore 的内存实现，复刻 postgres 版的原子语义
// （在单个互斥锁下完成读-检查-写，等价于 SELECT ... FOR UPDATE）。
type StarsStore struct {
	mu     sync.Mutex
	states map[int64]*starsState
	nextID int64
}

type starsState struct {
	balance int64
	granted bool
	txns    []domain.StarsTransaction // 追加序，读时倒序
}

// NewStarsStore 创建内存 StarsStore。
func NewStarsStore() *StarsStore {
	return &StarsStore{states: make(map[int64]*starsState)}
}

func (s *StarsStore) GetBalance(_ context.Context, userID int64) (domain.StarsBalance, error) {
	if userID == 0 {
		return domain.StarsBalance{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.states[userID]
	if st == nil {
		return domain.StarsBalance{UserID: userID}, nil
	}
	return domain.StarsBalance{UserID: userID, Balance: st.balance, Granted: st.granted}, nil
}

func (s *StarsStore) EnsureGrant(_ context.Context, userID, amount int64, date int) (domain.StarsBalance, bool, error) {
	if userID == 0 {
		return domain.StarsBalance{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.states[userID]
	if st == nil {
		st = &starsState{}
		s.states[userID] = st
	}
	if amount <= 0 {
		return domain.StarsBalance{UserID: userID, Balance: st.balance, Granted: st.granted}, false, nil
	}
	if st.granted {
		return domain.StarsBalance{UserID: userID, Balance: st.balance, Granted: true}, false, nil
	}
	st.balance += amount
	st.granted = true
	s.appendTxn(st, userID, amount, domain.StarsReasonGrant, domain.Peer{}, date, "", "")
	return domain.StarsBalance{UserID: userID, Balance: st.balance, Granted: true}, true, nil
}

func (s *StarsStore) Credit(_ context.Context, userID, amount int64, reason domain.StarsTransactionReason, peer domain.Peer, date int, title, desc string) (domain.StarsBalance, error) {
	if userID == 0 || amount <= 0 {
		return domain.StarsBalance{}, domain.ErrStarsInvalidAmount
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.states[userID]
	if st == nil {
		st = &starsState{}
		s.states[userID] = st
	}
	st.balance += amount
	s.appendTxn(st, userID, amount, reason, peer, date, title, desc)
	return domain.StarsBalance{UserID: userID, Balance: st.balance, Granted: st.granted}, nil
}

func (s *StarsStore) Debit(_ context.Context, userID, amount int64, reason domain.StarsTransactionReason, peer domain.Peer, date int, title, desc string) (domain.StarsBalance, error) {
	if userID == 0 || amount <= 0 {
		return domain.StarsBalance{}, domain.ErrStarsInvalidAmount
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.states[userID]
	if st == nil || st.balance < amount {
		return domain.StarsBalance{}, domain.ErrStarsInsufficient
	}
	st.balance -= amount
	s.appendTxn(st, userID, -amount, reason, peer, date, title, desc)
	return domain.StarsBalance{UserID: userID, Balance: st.balance, Granted: st.granted}, nil
}

func (s *StarsStore) ListTransactions(_ context.Context, userID int64, offset string, limit int) (domain.StarsTransactionPage, error) {
	if userID == 0 {
		return domain.StarsTransactionPage{}, nil
	}
	if limit <= 0 || limit > domain.MaxStarsTransactionsLimit {
		limit = domain.MaxStarsTransactionsLimit
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.states[userID]
	if st == nil {
		return domain.StarsTransactionPage{}, nil
	}
	page := domain.StarsTransactionPage{Balance: st.balance}
	cursor, hasCursor := domain.DecodeStarsCursor(offset)
	// 倒序遍历（id DESC）。
	out := make([]domain.StarsTransaction, 0, limit)
	for i := len(st.txns) - 1; i >= 0; i-- {
		t := st.txns[i]
		if hasCursor && t.ID >= cursor {
			continue
		}
		out = append(out, t)
		if len(out) == limit {
			// 还有更早的流水则给出下一页游标。
			if i-1 >= 0 {
				page.NextOffset = domain.EncodeStarsCursor(t.ID)
			}
			break
		}
	}
	page.Transactions = out
	return page, nil
}

func (s *StarsStore) TotalSpent(_ context.Context, userID int64) (int64, error) {
	if userID == 0 {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.states[userID]
	if st == nil {
		return 0, nil
	}
	var spent int64
	for _, txn := range st.txns {
		if txn.Amount < 0 {
			spent += -txn.Amount
		}
	}
	return spent, nil
}

func (s *StarsStore) appendTxn(st *starsState, userID, amount int64, reason domain.StarsTransactionReason, peer domain.Peer, date int, title, desc string) {
	s.nextID++
	st.txns = append(st.txns, domain.StarsTransaction{
		ID:          s.nextID,
		UserID:      userID,
		Peer:        peer,
		Amount:      amount,
		Date:        date,
		Reason:      reason,
		Title:       title,
		Description: desc,
	})
}
