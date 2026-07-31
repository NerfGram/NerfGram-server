package postgres

import (
	"context"

	"telesrv/internal/domain"
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

// ByOwners batch-loads account flags for a set of user IDs. Users with no
// row are simply absent from the result.
func (s *UserFlagsStore) ByOwners(ctx context.Context, userIDs []int64) (map[int64]domain.UserAccountFlags, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `SELECT user_id, fake, main_profile_tab FROM public.user_flags WHERE user_id = ANY($1)`, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]domain.UserAccountFlags, len(userIDs))
	for rows.Next() {
		var id int64
		var flags domain.UserAccountFlags
		if err := rows.Scan(&id, &flags.Fake, &flags.MainProfileTab); err != nil {
			return nil, err
		}
		out[id] = flags
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

// SetMainProfileTab stores the user's preferred main profile tab.
func (s *UserFlagsStore) SetMainProfileTab(ctx context.Context, userID int64, tab string) error {
	_, err := s.db.Exec(ctx, `
INSERT INTO public.user_flags (user_id, main_profile_tab, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (user_id) DO UPDATE SET main_profile_tab = EXCLUDED.main_profile_tab, updated_at = now()`,
		userID, tab)
	return err
}
