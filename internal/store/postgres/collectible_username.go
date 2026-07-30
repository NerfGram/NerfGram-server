package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"telesrv/internal/domain"
	"telesrv/internal/store/postgres/sqlcgen"
)

// CollectibleUsernameStore implements admin-assigned Fragment-style
// collectible username metadata (backs fragment.getCollectibleInfo).
type CollectibleUsernameStore struct {
	db sqlcgen.DBTX
}

// NewCollectibleUsernameStore creates a CollectibleUsernameStore from a pgx
// connection pool (or transaction).
func NewCollectibleUsernameStore(db sqlcgen.DBTX) *CollectibleUsernameStore {
	return &CollectibleUsernameStore{db: db}
}

func normalizeCollectibleUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(username, "@")))
}

// Get returns the collectible metadata for a username, if any admin has set
// it. domain.ErrCollectibleUsernameNotFound is returned for the normal case
// of a plain (non-collectible) username.
func (s *CollectibleUsernameStore) Get(ctx context.Context, username string) (domain.CollectibleUsername, bool, error) {
	normalized := normalizeCollectibleUsername(username)
	if normalized == "" {
		return domain.CollectibleUsername{}, false, nil
	}
	const query = `
SELECT username, purchase_date, currency, amount, crypto_currency, crypto_amount, url
FROM public.collectible_usernames
WHERE username = $1`
	var cu domain.CollectibleUsername
	err := s.db.QueryRow(ctx, query, normalized).Scan(
		&cu.Username, &cu.PurchaseDate, &cu.Currency, &cu.Amount, &cu.CryptoCurrency, &cu.CryptoAmount, &cu.URL,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CollectibleUsername{}, false, nil
	}
	if err != nil {
		return domain.CollectibleUsername{}, false, err
	}
	return cu, true, nil
}

// Set creates or updates the collectible metadata for a username.
// PurchaseDate defaults to now (unix seconds) if left zero.
func (s *CollectibleUsernameStore) Set(ctx context.Context, cu domain.CollectibleUsername, createdBy string) (domain.CollectibleUsername, error) {
	normalized := normalizeCollectibleUsername(cu.Username)
	if normalized == "" {
		return domain.CollectibleUsername{}, errors.New("username is required")
	}
	cu.Username = normalized
	if cu.PurchaseDate == 0 {
		cu.PurchaseDate = time.Now().Unix()
	}
	const query = `
INSERT INTO public.collectible_usernames (username, purchase_date, currency, amount, crypto_currency, crypto_amount, url, created_by, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
ON CONFLICT (username) DO UPDATE SET
    purchase_date = EXCLUDED.purchase_date,
    currency = EXCLUDED.currency,
    amount = EXCLUDED.amount,
    crypto_currency = EXCLUDED.crypto_currency,
    crypto_amount = EXCLUDED.crypto_amount,
    url = EXCLUDED.url,
    updated_at = now()
RETURNING username, purchase_date, currency, amount, crypto_currency, crypto_amount, url`
	var out domain.CollectibleUsername
	err := s.db.QueryRow(ctx, query,
		normalized, cu.PurchaseDate, cu.Currency, cu.Amount, cu.CryptoCurrency, cu.CryptoAmount, cu.URL, createdBy,
	).Scan(&out.Username, &out.PurchaseDate, &out.Currency, &out.Amount, &out.CryptoCurrency, &out.CryptoAmount, &out.URL)
	if err != nil {
		return domain.CollectibleUsername{}, err
	}
	return out, nil
}

// Delete removes collectible metadata for a username, reverting it to a
// plain username.
func (s *CollectibleUsernameStore) Delete(ctx context.Context, username string) error {
	normalized := normalizeCollectibleUsername(username)
	if normalized == "" {
		return nil
	}
	_, err := s.db.Exec(ctx, `DELETE FROM public.collectible_usernames WHERE username = $1`, normalized)
	return err
}
