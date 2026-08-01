package postgres

import (
	"context"
	"errors"

	"telesrv/internal/domain"
	"telesrv/internal/store/postgres/sqlcgen"
)

// UserVerificationStore holds admin-issued "Verified by <org>" badges
// (tg.UserFull.bot_verification) -- distinct from the plain Verified flag
// on the base User object.
type UserVerificationStore struct {
	db sqlcgen.DBTX
}

// NewUserVerificationStore creates a UserVerificationStore from a pgx
// connection pool.
func NewUserVerificationStore(db sqlcgen.DBTX) *UserVerificationStore {
	return &UserVerificationStore{db: db}
}

// ByOwners batch-loads each user's verification badge, for populating
// domain.User when serializing many users at once.
func (s *UserVerificationStore) ByOwners(ctx context.Context, userIDs []int64) (map[int64]domain.UserVerification, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `SELECT user_id, bot_id, icon, description FROM public.user_verifications WHERE user_id = ANY($1)`, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]domain.UserVerification, len(userIDs))
	for rows.Next() {
		var v domain.UserVerification
		if err := rows.Scan(&v.UserID, &v.BotID, &v.Icon, &v.Description); err != nil {
			return nil, err
		}
		out[v.UserID] = v
	}
	return out, rows.Err()
}

// Set creates or updates a user's verification badge.
func (s *UserVerificationStore) Set(ctx context.Context, v domain.UserVerification, createdBy string) error {
	if v.UserID <= 0 {
		return errors.New("user_id is required")
	}
	if v.BotID <= 0 {
		return errors.New("bot_id is required")
	}
	_, err := s.db.Exec(ctx, `
INSERT INTO public.user_verifications (user_id, bot_id, icon, description, created_by, updated_at)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (user_id) DO UPDATE SET
    bot_id = EXCLUDED.bot_id, icon = EXCLUDED.icon, description = EXCLUDED.description, updated_at = now()`,
		v.UserID, v.BotID, v.Icon, v.Description, createdBy)
	return err
}

// Remove revokes a user's verification badge.
func (s *UserVerificationStore) Remove(ctx context.Context, userID int64) error {
	_, err := s.db.Exec(ctx, `DELETE FROM public.user_verifications WHERE user_id = $1`, userID)
	return err
}
