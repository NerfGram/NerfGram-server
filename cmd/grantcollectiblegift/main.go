// Command grantcollectiblegift grants a catalog gift and upgrades it with
// specific collectible traits (dev/admin helper).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"telesrv/internal/domain"
	"telesrv/internal/store/postgres"
)

func main() {
	adminURL := flag.String("admin", "http://localhost:2599", "admin API base URL")
	adminToken := flag.String("token", "changeme_admin_api_token", "admin API bearer token")
	dsn := flag.String("dsn", "postgres://telesrv:telesrv@localhost:5432/telesrv?sslmode=disable", "postgres DSN")
	username := flag.String("username", "", "recipient @username")
	giftID := flag.Int64("gift-id", 0, "catalog gift_id")
	model := flag.String("model", "", "collectible model name")
	pattern := flag.String("pattern", "", "collectible pattern name")
	backdrop := flag.String("backdrop", "", "collectible backdrop name")
	flag.Parse()
	if strings.TrimSpace(*username) == "" || *giftID <= 0 || *model == "" || *pattern == "" || *backdrop == "" {
		fmt.Fprintln(os.Stderr, "usage: grantcollectiblegift -username denis0001_dev -gift-id 9000000000000026 -model Major -pattern Smartphone -backdrop \"Electric Purple\"")
		os.Exit(2)
	}

	ctx := context.Background()
	pool, err := postgres.Open(ctx, *dsn)
	if err != nil {
		fatal("open postgres: %v", err)
	}
	defer pool.Close()

	userID, err := lookupUserID(ctx, pool, strings.TrimPrefix(strings.ToLower(strings.TrimSpace(*username)), "@"))
	if err != nil {
		fatal("%v", err)
	}
	modelID, patternID, backdropID, upgradeStars, err := lookupTraits(ctx, pool, *giftID, *model, *pattern, *backdrop)
	if err != nil {
		fatal("%v", err)
	}

	savedID, err := grantGift(*adminURL, *adminToken, userID, *username, *giftID)
	if err != nil {
		fatal("grant gift: %v", err)
	}
	var msgID int
	if err := pool.QueryRow(ctx, `SELECT msg_id FROM peer_star_gifts WHERE id=$1`, savedID).Scan(&msgID); err != nil {
		fatal("lookup msg_id: %v", err)
	}
	fmt.Printf("granted gift_id=%d saved_gift_id=%d msg_id=%d\n", *giftID, savedID, msgID)

	stars := postgres.NewStarsStore(pool)
	if _, _, err := stars.EnsureGrant(ctx, userID, upgradeStars+1000, int(time.Now().Unix())); err != nil {
		fatal("credit upgrade stars: %v", err)
	}

	messages := postgres.NewMessageStore(pool)
	upgrades := postgres.NewStarGiftUpgradeStore(pool, messages)
	commandKey := fmt.Sprintf("admin-collectible-grant:%d:%d:%d", userID, savedID, time.Now().Unix())
	result, err := upgrades.UpgradeStarGift(ctx, domain.StarGiftUpgradeRequest{
		UserID:                   userID,
		Ref:                      domain.SavedStarGiftRef{Owner: domain.Peer{Type: domain.PeerTypeUser, ID: userID}, MsgID: msgID},
		ChargeStars:              upgradeStars,
		FormID:                   time.Now().UnixNano(),
		CommandKey:               commandKey,
		Date:                     int(time.Now().Unix()),
		ForceModelAttributeID:    modelID,
		ForcePatternAttributeID:  patternID,
		ForceBackdropAttributeID: backdropID,
	})
	if err != nil {
		fatal("upgrade gift: %v", err)
	}
	fmt.Printf("upgraded unique_id=%d slug=%s model=%q pattern=%q backdrop=%q\n",
		result.Unique.ID, result.Unique.Slug,
		result.Unique.Model.Name, result.Unique.Pattern.Name, result.Unique.Backdrop.Name)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func lookupUserID(ctx context.Context, pool *pgxpool.Pool, username string) (int64, error) {
	var userID int64
	err := pool.QueryRow(ctx, `
SELECT u.id FROM users u
LEFT JOIN peer_usernames p ON p.peer_type='user' AND p.peer_id=u.id
WHERE lower(u.username)=$1 OR p.username_lower=$1
LIMIT 1`, username).Scan(&userID)
	if err != nil {
		return 0, fmt.Errorf("lookup user %q: %w", username, err)
	}
	return userID, nil
}

func lookupTraits(ctx context.Context, pool *pgxpool.Pool, giftID int64, model, pattern, backdrop string) (modelID, patternID, backdropID, upgradeStars int64, err error) {
	var revisionID int64
	if err = pool.QueryRow(ctx, `
SELECT c.collectible_revision_id, rev.upgrade_stars
FROM star_gift_catalog c
JOIN star_gift_collectible_revisions rev ON rev.id = c.collectible_revision_id
WHERE c.gift_id=$1`, giftID).Scan(&revisionID, &upgradeStars); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("lookup collectible revision for gift %d: %w", giftID, err)
	}
	if err = pool.QueryRow(ctx, `SELECT id FROM star_gift_collectible_models WHERE collectible_revision_id=$1 AND name=$2`, revisionID, model).Scan(&modelID); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("lookup model %q: %w", model, err)
	}
	if err = pool.QueryRow(ctx, `SELECT id FROM star_gift_collectible_patterns WHERE collectible_revision_id=$1 AND name=$2`, revisionID, pattern).Scan(&patternID); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("lookup pattern %q: %w", pattern, err)
	}
	if err = pool.QueryRow(ctx, `SELECT id FROM star_gift_collectible_backdrops WHERE collectible_revision_id=$1 AND name=$2`, revisionID, backdrop).Scan(&backdropID); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("lookup backdrop %q: %w", backdrop, err)
	}
	return modelID, patternID, backdropID, upgradeStars, nil
}

func grantGift(adminURL, token string, userID int64, username string, giftID int64) (savedID int64, err error) {
	body, _ := json.Marshal(map[string]any{
		"username":   strings.TrimPrefix(username, "@"),
		"gift_id":    giftID,
		"command_id": fmt.Sprintf("grant-collectible:%d:%d", userID, time.Now().UnixNano()),
		"actor":      "grantcollectiblegift",
		"reason":     "admin collectible grant",
	})
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(adminURL, "/")+"/v1/accounts/grant-star-gift", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("admin grant failed (%d): %s", resp.StatusCode, string(raw))
	}
	var result struct {
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return 0, fmt.Errorf("decode grant response: %w", err)
	}
	savedID = parseInt64(result.Details["saved_gift_id"])
	if savedID <= 0 {
		return 0, fmt.Errorf("grant response missing saved_gift_id: %s", string(raw))
	}
	return savedID, nil
}

func parseInt64(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	default:
		return 0
	}
}
