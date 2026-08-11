package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"telesrv/internal/domain"
	"telesrv/internal/store"
	"telesrv/internal/store/postgres/sqlcgen"
)

type CollectiblePhoneStore struct{ db sqlcgen.DBTX }

func NewCollectiblePhoneStore(db sqlcgen.DBTX) *CollectiblePhoneStore {
	return &CollectiblePhoneStore{db: db}
}

var _ store.CollectiblePhoneStore = (*CollectiblePhoneStore)(nil)

const collectiblePhoneColumns = `id, phone, tier, status, owner_user_id, purchase_date,
currency, amount, crypto_currency, crypto_amount, url, original_owner_user_id,
transfer_count, version, created_at, updated_at`

type collectiblePhoneScanner interface{ Scan(...any) error }

func scanCollectiblePhone(row collectiblePhoneScanner) (domain.CollectiblePhone, error) {
	var a domain.CollectiblePhone
	err := row.Scan(&a.ID, &a.Phone, &a.Tier, &a.Status, &a.OwnerUserID, &a.PurchaseDate,
		&a.Currency, &a.Amount, &a.CryptoCurrency, &a.CryptoAmount, &a.URL,
		&a.OriginalOwnerUserID, &a.TransferCount, &a.Version, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

func normalizePhoneCommand(phone, actor, reason, key string) (string, string, string, string) {
	return domain.NormalizeCollectiblePhone(phone), strings.TrimSpace(actor), strings.TrimSpace(reason), strings.TrimSpace(key)
}

func ensureCollectiblePhoneOwner(ctx context.Context, tx pgx.Tx, userID int64) error {
	var bot, deleted bool
	err := tx.QueryRow(ctx, `SELECT is_bot, deleted_at IS NOT NULL FROM users WHERE id=$1`, userID).Scan(&bot, &deleted)
	if errors.Is(err, pgx.ErrNoRows) || bot || deleted {
		return domain.ErrCollectiblePhoneInvalid
	}
	if err != nil {
		return fmt.Errorf("check collectible phone owner: %w", err)
	}
	var occupied bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM collectible_phones WHERE status='owned' AND owner_user_id=$1)`, userID).Scan(&occupied); err != nil {
		return fmt.Errorf("check collectible phone owner limit: %w", err)
	}
	if occupied {
		return domain.ErrCollectiblePhoneOwnerLimit
	}
	return nil
}

func replayCollectiblePhone(ctx context.Context, tx pgx.Tx, key string) (domain.CollectiblePhone, bool, error) {
	if key == "" {
		return domain.CollectiblePhone{}, false, nil
	}
	row := tx.QueryRow(ctx, `SELECT cp.id, cp.phone, cp.tier, cp.status, cp.owner_user_id, cp.purchase_date,
cp.currency, cp.amount, cp.crypto_currency, cp.crypto_amount, cp.url, cp.original_owner_user_id,
cp.transfer_count, cp.version, cp.created_at, cp.updated_at
FROM collectible_phone_transfers t JOIN collectible_phones cp ON cp.id=t.collectible_id
WHERE t.command_key=$1`, key)
	a, err := scanCollectiblePhone(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CollectiblePhone{}, false, nil
	}
	if err != nil {
		return domain.CollectiblePhone{}, false, fmt.Errorf("replay collectible phone command: %w", err)
	}
	return a, true, nil
}

func insertCollectiblePhoneTransfer(ctx context.Context, tx pgx.Tx, id int64, kind domain.CollectibleUsernameTransferKind, from, to int64, currency string, amount int64, actor, reason, key string) error {
	var nullableKey any
	if key != "" {
		nullableKey = key
	}
	_, err := tx.Exec(ctx, `INSERT INTO collectible_phone_transfers
(collectible_id,kind,from_user_id,to_user_id,currency,amount,actor,reason,command_key,created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,now())`, id, string(kind), from, to, currency, amount, actor, reason, nullableKey)
	if err != nil {
		return fmt.Errorf("insert collectible phone transfer: %w", err)
	}
	return nil
}

func (s *CollectiblePhoneStore) MintCollectiblePhone(ctx context.Context, req domain.MintCollectiblePhoneRequest) (domain.CollectiblePhone, bool, error) {
	if s == nil || s.db == nil {
		return domain.CollectiblePhone{}, false, fmt.Errorf("collectible phone store is not configured")
	}
	req.Phone, req.Actor, req.Reason, req.CommandKey = normalizePhoneCommand(req.Phone, req.Actor, req.Reason, req.CommandKey)
	if req.Tier == "" {
		req.Tier = domain.CollectiblePhoneTierStandard
	}
	if req.Currency == "" {
		req.Currency = domain.CollectibleCurrencyUSD
	}
	if req.URL == "" {
		req.URL = "https://fragment.com/number/" + req.Phone
	}
	if err := req.Validate(); err != nil {
		return domain.CollectiblePhone{}, false, err
	}
	var out domain.CollectiblePhone
	created := false
	err := withTx(ctx, s.db, "mint collectible phone", func(tx pgx.Tx) error {
		if replay, found, err := replayCollectiblePhone(ctx, tx, req.CommandKey); err != nil {
			return err
		} else if found {
			out = replay
			return nil
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM collectible_phones WHERE phone=$1 AND status<>'burned')`, req.Phone).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return domain.ErrCollectiblePhoneInvalid
		}
		if req.OwnerUserID > 0 {
			if err := ensureCollectiblePhoneOwner(ctx, tx, req.OwnerUserID); err != nil {
				return err
			}
		}
		status := domain.CollectibleUsernameStatusVault
		if req.OwnerUserID > 0 {
			status = domain.CollectibleUsernameStatusOwned
		}
		purchaseDate := req.PurchaseDate.UTC()
		if req.PurchaseDate.IsZero() {
			purchaseDate = time.Now().UTC()
		}
		row := tx.QueryRow(ctx, `INSERT INTO collectible_phones
(phone,tier,status,owner_user_id,purchase_date,currency,amount,crypto_currency,crypto_amount,url,
 original_owner_user_id,transfer_count,version,created_at,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$4,0,1,now(),now()) RETURNING `+collectiblePhoneColumns,
			req.Phone, string(req.Tier), string(status), req.OwnerUserID, purchaseDate, req.Currency, req.Amount, req.CryptoCurrency, req.CryptoAmount, req.URL)
		var err error
		out, err = scanCollectiblePhone(row)
		if err != nil {
			if isUniqueViolation(err) {
				return domain.ErrCollectiblePhoneOwnerLimit
			}
			return err
		}
		if err := insertCollectiblePhoneTransfer(ctx, tx, out.ID, domain.CollectibleUsernameKindMint, 0, req.OwnerUserID, req.Currency, req.Amount, req.Actor, req.Reason, req.CommandKey); err != nil {
			return err
		}
		created = true
		return nil
	})
	return out, created, err
}

func (s *CollectiblePhoneStore) UpdateCollectiblePhonePrice(ctx context.Context, req domain.UpdateCollectiblePhonePriceRequest) (domain.CollectiblePhone, bool, error) {
	if s == nil || s.db == nil {
		return domain.CollectiblePhone{}, false, fmt.Errorf("collectible phone store is not configured")
	}
	req.Phone = domain.NormalizeCollectiblePhone(req.Phone)
	req.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))
	req.CryptoCurrency = strings.ToUpper(strings.TrimSpace(req.CryptoCurrency))
	req.Actor = strings.TrimSpace(req.Actor)
	req.Reason = strings.TrimSpace(req.Reason)
	if err := req.Validate(); err != nil {
		return domain.CollectiblePhone{}, false, err
	}
	var out domain.CollectiblePhone
	changed := false
	err := withTx(ctx, s.db, "update collectible phone price", func(tx pgx.Tx) error {
		a, err := scanCollectiblePhone(tx.QueryRow(ctx, `SELECT `+collectiblePhoneColumns+` FROM collectible_phones WHERE phone=$1 AND status<>'burned' FOR UPDATE`, req.Phone))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrCollectiblePhoneNotFound
		}
		if err != nil {
			return err
		}
		if a.Currency == req.Currency && a.Amount == req.Amount && a.CryptoCurrency == req.CryptoCurrency && a.CryptoAmount == req.CryptoAmount {
			out = a
			return nil
		}
		out, err = scanCollectiblePhone(tx.QueryRow(ctx, `UPDATE collectible_phones SET currency=$2, amount=$3,
crypto_currency=$4, crypto_amount=$5, version=version+1, updated_at=now()
WHERE id=$1 RETURNING `+collectiblePhoneColumns, a.ID, req.Currency, req.Amount, req.CryptoCurrency, req.CryptoAmount))
		if err != nil {
			return err
		}
		changed = true
		return nil
	})
	return out, changed, err
}

func (s *CollectiblePhoneStore) TransferCollectiblePhone(ctx context.Context, req domain.TransferCollectiblePhoneRequest) (domain.CollectiblePhone, bool, error) {
	req.Phone, req.Actor, req.Reason, req.CommandKey = normalizePhoneCommand(req.Phone, req.Actor, req.Reason, req.CommandKey)
	if err := req.Validate(); err != nil {
		return domain.CollectiblePhone{}, false, err
	}
	var out domain.CollectiblePhone
	changed := false
	err := withTx(ctx, s.db, "transfer collectible phone", func(tx pgx.Tx) error {
		if replay, found, err := replayCollectiblePhone(ctx, tx, req.CommandKey); err != nil {
			return err
		} else if found {
			out = replay
			return nil
		}
		a, err := scanCollectiblePhone(tx.QueryRow(ctx, `SELECT `+collectiblePhoneColumns+` FROM collectible_phones WHERE phone=$1 AND status<>'burned' FOR UPDATE`, req.Phone))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrCollectiblePhoneNotFound
		}
		if err != nil {
			return err
		}
		if a.OwnerUserID == req.ToUserID {
			out = a
			return nil
		}
		if err := ensureCollectiblePhoneOwner(ctx, tx, req.ToUserID); err != nil {
			return err
		}
		original := a.OriginalOwnerUserID
		if original == 0 {
			original = req.ToUserID
		}
		out, err = scanCollectiblePhone(tx.QueryRow(ctx, `UPDATE collectible_phones SET status='owned', owner_user_id=$2,
original_owner_user_id=$3, transfer_count=transfer_count+1, version=version+1, updated_at=now()
WHERE id=$1 RETURNING `+collectiblePhoneColumns, a.ID, req.ToUserID, original))
		if err != nil {
			if isUniqueViolation(err) {
				return domain.ErrCollectiblePhoneOwnerLimit
			}
			return err
		}
		if err := insertCollectiblePhoneTransfer(ctx, tx, a.ID, domain.CollectibleUsernameKindTransfer, a.OwnerUserID, req.ToUserID, "", 0, req.Actor, req.Reason, req.CommandKey); err != nil {
			return err
		}
		changed = true
		return nil
	})
	return out, changed, err
}

func (s *CollectiblePhoneStore) RevokeCollectiblePhone(ctx context.Context, req domain.RevokeCollectiblePhoneRequest) (domain.CollectiblePhone, bool, error) {
	req.Phone, req.Actor, req.Reason, req.CommandKey = normalizePhoneCommand(req.Phone, req.Actor, req.Reason, req.CommandKey)
	if err := req.Validate(); err != nil {
		return domain.CollectiblePhone{}, false, err
	}
	var out domain.CollectiblePhone
	changed := false
	err := withTx(ctx, s.db, "revoke collectible phone", func(tx pgx.Tx) error {
		if replay, found, err := replayCollectiblePhone(ctx, tx, req.CommandKey); err != nil {
			return err
		} else if found {
			out = replay
			return nil
		}
		a, err := scanCollectiblePhone(tx.QueryRow(ctx, `SELECT `+collectiblePhoneColumns+` FROM collectible_phones WHERE phone=$1 AND status<>'burned' FOR UPDATE`, req.Phone))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrCollectiblePhoneNotFound
		}
		if err != nil {
			return err
		}
		if a.Status == domain.CollectibleUsernameStatusVault && !req.Burn {
			out = a
			return nil
		}
		status := domain.CollectibleUsernameStatusVault
		kind := domain.CollectibleUsernameKindRevoke
		if req.Burn {
			status = domain.CollectibleUsernameStatusBurned
			kind = domain.CollectibleUsernameKindBurn
		}
		out, err = scanCollectiblePhone(tx.QueryRow(ctx, `UPDATE collectible_phones SET status=$2, owner_user_id=0,
version=version+1, updated_at=now() WHERE id=$1 RETURNING `+collectiblePhoneColumns, a.ID, string(status)))
		if err != nil {
			return err
		}
		if err := insertCollectiblePhoneTransfer(ctx, tx, a.ID, kind, a.OwnerUserID, 0, "", 0, req.Actor, req.Reason, req.CommandKey); err != nil {
			return err
		}
		changed = true
		return nil
	})
	return out, changed, err
}

func (s *CollectiblePhoneStore) DeleteCollectiblePhone(ctx context.Context, req domain.DeleteCollectiblePhoneRequest) (bool, error) {
	req.Phone, req.Actor, req.Reason, req.CommandKey = normalizePhoneCommand(req.Phone, req.Actor, req.Reason, req.CommandKey)
	if err := req.Validate(); err != nil {
		return false, err
	}
	tag, err := s.db.Exec(ctx, `DELETE FROM collectible_phones WHERE id=(SELECT id FROM collectible_phones WHERE phone=$1 ORDER BY id DESC LIMIT 1)`, req.Phone)
	if err != nil {
		return false, fmt.Errorf("delete collectible phone: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (s *CollectiblePhoneStore) CollectiblePhone(ctx context.Context, phone string) (domain.CollectiblePhone, error) {
	phone = domain.NormalizeCollectiblePhone(phone)
	if !domain.ValidCollectiblePhone(phone) {
		return domain.CollectiblePhone{}, domain.ErrCollectiblePhoneInvalid
	}
	a, err := scanCollectiblePhone(s.db.QueryRow(ctx, `SELECT `+collectiblePhoneColumns+` FROM collectible_phones WHERE phone=$1 AND status<>'burned' ORDER BY id DESC LIMIT 1`, phone))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CollectiblePhone{}, domain.ErrCollectiblePhoneNotFound
	}
	return a, err
}

func (s *CollectiblePhoneStore) CollectiblePhoneByID(ctx context.Context, id int64) (domain.CollectiblePhone, error) {
	a, err := scanCollectiblePhone(s.db.QueryRow(ctx, `SELECT `+collectiblePhoneColumns+` FROM collectible_phones WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CollectiblePhone{}, domain.ErrCollectiblePhoneNotFound
	}
	return a, err
}

func (s *CollectiblePhoneStore) OwnedCollectiblePhones(ctx context.Context, userIDs []int64) (map[int64]domain.CollectiblePhone, error) {
	out := make(map[int64]domain.CollectiblePhone)
	if len(userIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.Query(ctx, `SELECT `+collectiblePhoneColumns+` FROM collectible_phones WHERE status='owned' AND owner_user_id=ANY($1::bigint[])`, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		a, err := scanCollectiblePhone(rows)
		if err != nil {
			return nil, err
		}
		out[a.OwnerUserID] = a
	}
	return out, rows.Err()
}

func (s *CollectiblePhoneStore) ListCollectiblePhones(ctx context.Context, f domain.CollectiblePhoneFilter) ([]domain.CollectiblePhone, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 200 {
		f.Limit = 200
	}
	query := strings.TrimPrefix(domain.NormalizeCollectiblePhone(f.Query), "")
	rows, err := s.db.Query(ctx, `SELECT `+collectiblePhoneColumns+` FROM collectible_phones
WHERE ($1='' OR status=$1) AND ($2='' OR tier=$2) AND ($3::bigint=0 OR owner_user_id=$3)
AND ($4='' OR phone LIKE $4||'%') AND ($5::bigint=0 OR id<$5) ORDER BY id DESC LIMIT $6`,
		string(f.Status), string(f.Tier), f.OwnerUserID, query, f.BeforeID, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.CollectiblePhone, 0, f.Limit)
	for rows.Next() {
		a, err := scanCollectiblePhone(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *CollectiblePhoneStore) CollectiblePhoneTransfers(ctx context.Context, collectibleID int64, limit int) ([]domain.CollectiblePhoneTransfer, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.db.Query(ctx, `SELECT id,collectible_id,kind,from_user_id,to_user_id,currency,amount,actor,reason,COALESCE(command_key,''),created_at
FROM collectible_phone_transfers WHERE collectible_id=$1 ORDER BY id DESC LIMIT $2`, collectibleID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.CollectiblePhoneTransfer, 0)
	for rows.Next() {
		var v domain.CollectiblePhoneTransfer
		if err := rows.Scan(&v.ID, &v.CollectibleID, &v.Kind, &v.FromUserID, &v.ToUserID, &v.Currency, &v.Amount, &v.Actor, &v.Reason, &v.CommandKey, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
