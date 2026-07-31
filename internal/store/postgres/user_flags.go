package postgres

import (
	"context"

	"telesrv/internal/store/postgres/sqlcgen"
)

// UserFlagsStore holds small admin-side annotations on user accounts (like
// the "Fake" badge) that live outside the sqlc-generated base user query.
type UserFlagsStore struct {
	db sqlcgen.DBTX
}

// NewUserFlagsStore creates a UserFlagsStore from a pgx connection pool.
func NewUserFlagsStore(db sqlcgen.DBTX) *UserFlagsStore {
	return &UserFlagsStore{db: db}
}

// ByOwners batch-loads the Fake flag for a set of user IDs. Users with no
// row (the common case) are simply absent from the result, i.e. not fake.
func (s *UserFlagsStore) ByOwners(ctx context.Context, userIDs []int64) (map[int64]bool, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `SELECT user_id, fake FROM public.user_flags WHERE user_id = ANY($1) AND fake`, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]bool, len(userIDs))
	for rows.Next() {
		var id int64
		var fake bool
		if err := rows.Scan(&id, &fake); err != nil {
			return nil, err
		}
		out[id] = fake
	}
	return out, rows.Err()
}

// SetFake creates or updates the Fake flag for a user.
func (s *UserFlagsStore) SetFake(ctx context.Context, userID int64, fake bool) error {
	_, err := s.db.Exec(ctx, `
INSERT INTO public.user_flags (user_id, fake, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (user_id) DO UPDATE SET fake = EXCLUDED.fake, updated_at = now()`,
		userID, fake)
	return err
}
