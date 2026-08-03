package files

import (
	"context"
	"fmt"
	"os"
	"time"

	"telesrv/internal/domain"
)

// SeedOfficialSystemAvatar idempotently seeds the built-in official system
// account's (777000) profile photo from the configured brand logo path, writing
// it under the fixed domain.OfficialSystemUserPhotoID so the photo/blob layer
// and the pure domain.OfficialSystemUser() struct literal stay in sync
// across restarts. It also registers the photo as the account's *current*
// profile photo (the profile_photos association) — without this, list
// views render the avatar from the hardcoded User struct fields, but
// users.getFullUser (triggered on chat open) reads only the association,
// finds nothing, and the client wipes the avatar it just showed.
// Returns true if it actually wrote a new photo.
func (s *Service) SeedOfficialSystemAvatar(ctx context.Context) (bool, error) {
	path := s.systemAvatarPath
	if path == "" {
		path = "downloads/FromGram.png"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read system avatar %q: %w", path, err)
	}
	photoID := domain.OfficialSystemUserPhotoID
	sizes, err := s.putAvatarStaticSizes(ctx, photoID, raw, photoSizeSpecsForAvatar(raw))
	if err != nil {
		return false, err
	}
	wrote := false
	if existing, found, err := s.media.GetPhoto(ctx, photoID); err != nil {
		return false, err
	} else if !found {
		photo := domain.Photo{
			ID:            photoID,
			AccessHash:    domain.OfficialSystemUserPhotoAccessHash,
			FileReference: randomFileReference(),
			Date:          int(time.Now().Unix()),
			DCID:          s.dc,
			Sizes:         sizes,
		}
		if err := s.media.PutPhoto(ctx, photo); err != nil {
			return false, err
		}
		wrote = true
	} else {
		existing.Sizes = sizes
		existing.Date = int(time.Now().Unix())
		if err := s.media.PutPhoto(ctx, existing); err != nil {
			return false, err
		}
		wrote = true
	}
	photo, ok, err := s.SetCurrentProfilePhoto(ctx, domain.PeerTypeUser, domain.OfficialSystemUserID, photoID, int(time.Now().Unix()))
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("official system avatar photo %d not found after seeding", photoID)
	}
	domain.SetOfficialSystemUserAvatar(photo.DCID, domain.StrippedFromSizes(photo.Sizes))
	return wrote, nil
}
