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

// CollectibleUsernameStore implements admin-issued Fragment-style collectible
// username metadata (backs fragment.getCollectibleInfo). A collectible
// username is a brand-new, additional username issued to an owner -- never a
// relabeling of their existing username -- so issuing one also claims it in
// the shared peer_usernames registry (the same uniqueness registry used by
// the owner's primary username and by channels).
type CollectibleUsernameStore struct {
	db sqlcgen.DBTX
}

// NewCollectibleUsernameStore creates a CollectibleUsernameStore from a pgx
// connection pool.
func NewCollectibleUsernameStore(db sqlcgen.DBTX) *CollectibleUsernameStore {
	return &CollectibleUsernameStore{db: db}
}

func normalizeCollectibleUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(username, "@")))
}

const collectibleUsernameColumns = `username, owner_user_id, active, purchase_date, currency, amount, crypto_currency, crypto_amount, url`

func scanCollectibleUsername(row pgx.Row) (domain.CollectibleUsername, error) {
	var cu domain.CollectibleUsername
	var ownerUserID *int64
	err := row.Scan(&cu.Username, &ownerUserID, &cu.Active, &cu.PurchaseDate, &cu.Currency, &cu.Amount, &cu.CryptoCurrency, &cu.CryptoAmount, &cu.URL)
	if err != nil {
		return domain.CollectibleUsername{}, err
	}
	if ownerUserID != nil {
		cu.OwnerUserID = *ownerUserID
	}
	return cu, nil
}

// Get returns the collectible metadata for a username, if any admin has
// issued it. domain.ErrCollectibleUsernameNotFound-equivalent (found=false)
// is returned for the normal case of a plain, non-collectible username.
func (s *CollectibleUsernameStore) Get(ctx context.Context, username string) (domain.CollectibleUsername, bool, error) {
	normalized := normalizeCollectibleUsername(username)
	if normalized == "" {
		return domain.CollectibleUsername{}, false, nil
	}
	query := `SELECT ` + collectibleUsernameColumns + ` FROM public.collectible_usernames WHERE username = $1`
	cu, err := scanCollectibleUsername(s.db.QueryRow(ctx, query, normalized))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CollectibleUsername{}, false, nil
	}
	if err != nil {
		return domain.CollectibleUsername{}, false, err
	}
	return cu, true, nil
}

// ByOwners batch-loads each owner's collectible username (at most one per
// owner), for populating domain.User when serializing many users at once.
func (s *CollectibleUsernameStore) ByOwners(ctx context.Context, ownerUserIDs []int64) (map[int64]domain.CollectibleUsername, error) {
	if len(ownerUserIDs) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `SELECT `+collectibleUsernameColumns+` FROM public.collectible_usernames WHERE owner_user_id = ANY($1) ORDER BY active DESC, purchase_date DESC`, ownerUserIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]domain.CollectibleUsername, len(ownerUserIDs))
	for rows.Next() {
		cu, err := scanCollectibleUsername(rows)
		if err != nil {
			return nil, err
		}
		if cu.OwnerUserID != 0 {
			if _, exists := out[cu.OwnerUserID]; !exists {
				out[cu.OwnerUserID] = cu
			}
		}
	}
	return out, rows.Err()
}

// Issue mints a brand-new collectible username and assigns it to ownerUserID.
// It claims the username in the shared peer_usernames registry (so
// @username resolution finds the owner) and records the collectible pricing
// metadata, atomically. Fails with domain.ErrUsernameOccupied if the
// username is already taken by anyone (including the same owner's existing
// primary username).
func (s *CollectibleUsernameStore) Issue(ctx context.Context, cu domain.CollectibleUsername, createdBy string) (domain.CollectibleUsername, error) {
	normalized := normalizeCollectibleUsername(cu.Username)
	if normalized == "" {
		return domain.CollectibleUsername{}, errors.New("username is required")
	}
	if cu.OwnerUserID <= 0 {
		return domain.CollectibleUsername{}, errors.New("owner_user_id is required")
	}
	cu.Username = normalized
	if cu.PurchaseDate == 0 {
		cu.PurchaseDate = time.Now().Unix()
	}
	var out domain.CollectibleUsername
	err := withTx(ctx, s.db, "issue collectible username", func(tx pgx.Tx) error {
		owner, found, err := getPeerUsernameOwner(ctx, tx, normalized, true)
		if err != nil {
			return err
		}
		if found && !owner.matches(peerUsernameTypeUser, cu.OwnerUserID) {
			return domain.ErrUsernameOccupied
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO peer_usernames (username_lower, peer_type, peer_id)
VALUES ($1, $2, $3)
ON CONFLICT (username_lower) DO UPDATE SET peer_type = EXCLUDED.peer_type, peer_id = EXCLUDED.peer_id`, normalized, peerUsernameTypeUser, cu.OwnerUserID); err != nil {
			return err
		}
		row := tx.QueryRow(ctx, `
INSERT INTO public.collectible_usernames (username, owner_user_id, active, purchase_date, currency, amount, crypto_currency, crypto_amount, url, created_by, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
ON CONFLICT (username) DO UPDATE SET
  owner_user_id = EXCLUDED.owner_user_id,
  active = EXCLUDED.active,
  purchase_date = EXCLUDED.purchase_date,
  currency = EXCLUDED.currency,
  amount = EXCLUDED.amount,
  crypto_currency = EXCLUDED.crypto_currency,
  crypto_amount = EXCLUDED.crypto_amount,
  url = EXCLUDED.url,
  created_by = EXCLUDED.created_by,
  updated_at = now()
RETURNING `+collectibleUsernameColumns,
			normalized, cu.OwnerUserID, cu.Active, cu.PurchaseDate, cu.Currency, cu.Amount, cu.CryptoCurrency, cu.CryptoAmount, cu.URL, createdBy)
		result, err := scanCollectibleUsername(row)
		if err != nil {
			return err
		}
		out = result
		return nil
	})
	if err != nil {
		return domain.CollectibleUsername{}, err
	}
	return out, nil
}

// SetActive switches whether a collectible username is the owner's currently
// active/shown username (vs. their original editable one). Only one
// collectible username per owner can be active at a time (enforced by a
// partial unique index), so activating one implicitly deactivates any other
// the same owner holds.
func (s *CollectibleUsernameStore) SetActive(ctx context.Context, username string, ownerUserID int64, active bool) error {
	normalized := normalizeCollectibleUsername(username)
	if normalized == "" {
		return errors.New("username is required")
	}
	return withTx(ctx, s.db, "set collectible username active", func(tx pgx.Tx) error {
		if active {
			if _, err := tx.Exec(ctx, `UPDATE public.collectible_usernames SET active = false, updated_at = now() WHERE owner_user_id = $1 AND active`, ownerUserID); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE public.collectible_usernames SET active = $3, updated_at = now() WHERE username = $1 AND owner_user_id = $2`, normalized, ownerUserID, active)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrCollectibleUsernameNotFound
		}
		return nil
	})
}

// Delete revokes a collectible username entirely: removes it from the
// shared peer_usernames registry (freeing the string for reuse) and deletes
// its collectible metadata.
func (s *CollectibleUsernameStore) Delete(ctx context.Context, username string) error {
	normalized := normalizeCollectibleUsername(username)
	if normalized == "" {
		return nil
	}
	return withTx(ctx, s.db, "delete collectible username", func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM public.collectible_usernames WHERE username = $1`, normalized); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM peer_usernames WHERE username_lower = $1 AND peer_type = $2`, normalized, peerUsernameTypeUser)
		return err
	})
}
