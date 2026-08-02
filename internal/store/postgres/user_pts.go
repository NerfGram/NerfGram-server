package postgres

import (
	"context"
	"fmt"

	"telesrv/internal/store/postgres/sqlcgen"
)

func normalizePtsCount(count int) int {
	if count <= 0 {
		return 1
	}
	return count
}

func ensureUserUpdateWatermark(ctx context.Context, db sqlcgen.DBTX, userID int64) error {
	if userID == 0 {
		return fmt.Errorf("user pts: missing user id")
	}
	_, err := db.Exec(ctx, `
INSERT INTO user_update_watermarks (user_id, contiguous_pts)
VALUES ($1, 0)
ON CONFLICT (user_id) DO NOTHING`, userID)
	if err != nil {
		return fmt.Errorf("ensure user update watermark: %w", err)
	}
	return nil
}

func reserveUserPts(ctx context.Context, db sqlcgen.DBTX, userID int64, count int) (int, error) {
	count = normalizePtsCount(count)
	if userID == 0 {
		return 0, fmt.Errorf("user pts: missing user id")
	}
	var pts int
	if err := db.QueryRow(ctx, `
INSERT INTO user_update_watermarks (user_id, contiguous_pts)
VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE
SET contiguous_pts = user_update_watermarks.contiguous_pts + EXCLUDED.contiguous_pts,
    updated_at = now()
RETURNING contiguous_pts`, userID, count).Scan(&pts); err != nil {
		return 0, fmt.Errorf("reserve user pts: %w", err)
	}
	return pts, nil
}

func advanceUserPtsTo(ctx context.Context, db sqlcgen.DBTX, userID int64, pts, count int) error {
	count = normalizePtsCount(count)
	if pts <= 0 {
		return fmt.Errorf("user pts: missing pts")
	}
	if err := ensureUserUpdateWatermark(ctx, db, userID); err != nil {
		return err
	}
	var current int
	if err := db.QueryRow(ctx, `
SELECT contiguous_pts
FROM user_update_watermarks
WHERE user_id = $1
FOR UPDATE`, userID).Scan(&current); err != nil {
		return fmt.Errorf("lock user update watermark: %w", err)
	}
	if current+count != pts {
		return fmt.Errorf("user pts not contiguous: user_id=%d current=%d pts=%d pts_count=%d", userID, current, pts, count)
	}
	if _, err := db.Exec(ctx, `
UPDATE user_update_watermarks
SET contiguous_pts = $2,
    updated_at = now()
WHERE user_id = $1`, userID, pts); err != nil {
		return fmt.Errorf("advance user pts watermark: %w", err)
	}
	return nil
}
