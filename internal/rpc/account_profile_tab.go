package rpc

import (
	"context"
	"errors"

	"github.com/iamxvbaba/td/tg"
)

func profileTabStorageKey(tab tg.ProfileTabClass) (string, error) {
	if tab == nil {
		return "", errors.New("profile tab required")
	}
	switch tab.(type) {
	case *tg.ProfileTabPosts:
		return "posts", nil
	case *tg.ProfileTabGifts:
		return "gifts", nil
	case *tg.ProfileTabMedia:
		return "media", nil
	case *tg.ProfileTabFiles:
		return "files", nil
	case *tg.ProfileTabMusic:
		return "music", nil
	case *tg.ProfileTabVoice:
		return "voice", nil
	case *tg.ProfileTabLinks:
		return "links", nil
	case *tg.ProfileTabGifs:
		return "gifs", nil
	default:
		return "", errors.New("unsupported profile tab")
	}
}

func tgProfileTabFromStorage(tab string) tg.ProfileTabClass {
	switch tab {
	case "posts":
		return &tg.ProfileTabPosts{}
	case "gifts":
		return &tg.ProfileTabGifts{}
	case "media":
		return &tg.ProfileTabMedia{}
	case "files":
		return &tg.ProfileTabFiles{}
	case "music":
		return &tg.ProfileTabMusic{}
	case "voice":
		return &tg.ProfileTabVoice{}
	case "links":
		return &tg.ProfileTabLinks{}
	case "gifs":
		return &tg.ProfileTabGifs{}
	default:
		return nil
	}
}

func (r *Router) onAccountSetMainProfileTab(ctx context.Context, req *tg.AccountSetMainProfileTabRequest) (bool, error) {
	userID, _, err := r.currentUserID(ctx)
	if err != nil {
		return false, internalErr()
	}
	if userID == 0 {
		return false, internalErr()
	}
	svc, ok := r.deps.Users.(UserIdentityService)
	if !ok {
		return false, notImplementedErr()
	}
	tabKey, err := profileTabStorageKey(req.Tab)
	if err != nil {
		return false, internalErr()
	}
	if _, err := svc.SetMainProfileTab(ctx, userID, tabKey); err != nil {
		return false, internalErr()
	}
	r.invalidateRPCProjectionForUser(userID)
	return true, nil
}
