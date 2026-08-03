package admin

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"telesrv/internal/domain"
	"telesrv/internal/officialgifts"
)

// OfficialStarGiftBootstrapStats summarizes automatic startup imports.
type OfficialStarGiftBootstrapStats struct {
	SkippedUnavailable int
	SkippedExcluded    int
	SkippedPresent     int
	ImportedRegular    int
	ImportedCollectible int
	Failed             int
}

// BootstrapOfficialStarGifts imports every official star gift and upgradeable
// NFT collectible from the local official-gifts snapshot. It is idempotent:
// already-imported gifts are skipped, and upgradeable gifts missing a
// collectible pool are backfilled.
func (s *Service) BootstrapOfficialStarGifts(ctx context.Context) (OfficialStarGiftBootstrapStats, error) {
	var stats OfficialStarGiftBootstrapStats
	if s == nil || s.officialGifts == nil || s.gifts == nil {
		return stats, nil
	}
	gifts, err := s.officialGifts.List(ctx)
	if errors.Is(err, officialgifts.ErrUnavailable) {
		stats.SkippedUnavailable = 1
		return stats, nil
	}
	if err != nil {
		return stats, err
	}
	imported, err := s.gifts.OfficialGiftImportIndex(ctx)
	if err != nil {
		return stats, err
	}
	sort.Slice(gifts, func(i, j int) bool {
		if gifts[i].Stars != gifts[j].Stars {
			return gifts[i].Stars < gifts[j].Stars
		}
		return gifts[i].ID < gifts[j].ID
	})
	sortOrder := len(imported) + 1
	for _, gift := range gifts {
		if officialgifts.ExcludedFromImport(gift.ID, gift.Title) {
			stats.SkippedExcluded++
			continue
		}
		state, ok := imported[gift.ID]
		includeCollectible := gift.CanUpgrade()
		if includeCollectible {
			if ok && state.HasCollectible {
				stats.SkippedPresent++
				continue
			}
		} else if ok {
			stats.SkippedPresent++
			continue
		}
		commandID := "bootstrap-official-" + strconv.FormatInt(gift.ID, 10)
		if includeCollectible {
			commandID += "-collectible"
		}
		_, err := s.ImportOfficialStarGift(ctx, ImportOfficialStarGiftRequest{
			CommandMeta: CommandMeta{
				CommandID: commandID,
				Actor:     "bootstrap",
				Reason:    "automatic startup import",
			},
			SourceGiftID:       strconv.FormatInt(gift.ID, 10),
			Title:              gift.Title,
			Stars:              gift.Stars,
			ConvertStars:       gift.ConvertStars,
			Enabled:            true,
			SortOrder:          sortOrder,
			IncludeCollectible: includeCollectible,
			UpgradeStars:       gift.UpgradeStars,
			SupplyTotal:        gift.AvailabilityTotal,
		})
		if err != nil {
			stats.Failed++
			continue
		}
		if includeCollectible {
			stats.ImportedCollectible++
		} else {
			stats.ImportedRegular++
		}
		imported[gift.ID] = domain.OfficialGiftImportState{
			Imported:       true,
			HasCollectible: includeCollectible,
		}
		sortOrder++
	}
	if stats.Failed > 0 {
		return stats, fmt.Errorf("official star gift bootstrap failed for %d gifts", stats.Failed)
	}
	return stats, nil
}
