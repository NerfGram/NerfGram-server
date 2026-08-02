package domain

import "telesrv/internal/branding"

const (
	// OfficialSystemUserID 是 Telegram 兼容客户端识别的官方系统账号。
	OfficialSystemUserID int64 = 777000
	// OfficialSystemUserPhotoID/AccessHash 是该账号头像 photo 的固定 id，
	// 与 files.Service.SeedOfficialSystemAvatar 种子写入的行保持一致，
	// 确保跨重启后 OfficialSystemUser() 引用的 photo id 稳定不变。
	OfficialSystemUserPhotoID         int64 = 7770000001
	OfficialSystemUserPhotoAccessHash int64 = 5837219004471160321

	// BotFatherUserID 是内置 BotFather 账号，与官方 @BotFather 同 ID。
	BotFatherUserID int64 = 93372553
	// BotFatherAccessHash 固定不变；与迁移 0090 的种子行双写，必须保持一致。
	BotFatherAccessHash int64 = 7421896403922962293

	// StickersBotUserID 是内置 @Stickers 账号。它是 server 内置 service bot，
	// 不走外部 Bot API 进程。
	StickersBotUserID int64 = 1063110917
	// StickersBotAccessHash 固定不变；与 postgres 种子行双写，必须保持一致。
	StickersBotAccessHash int64 = 5213187021149032991

	// ChatBotUserID 是内置 @ChatBot 账号。它把私聊文本转给 server AI provider 链。
	ChatBotUserID int64 = 1250000007
	// ChatBotAccessHash 固定不变；与 postgres 种子行双写，必须保持一致。
	ChatBotAccessHash int64 = 6332902371644871201

	// StarsTestBotUserID 是内置 @StarsTestBot，用于本地测试 Stars paid invite links。
	StarsTestBotUserID int64 = 1250000008
	// StarsTestBotAccessHash 固定不变；与 postgres 种子行双写，必须保持一致。
	StarsTestBotAccessHash int64 = 7129485031847261503

	// VerifyBotUserID is the built-in @verifybot: it collects official platform
	// verification applications and reports decisions back to the applicant. The
	// id is reserved and stable, so a restart never re-creates the account under a
	// different identity.
	VerifyBotUserID int64 = 1250000011
	// VerifyBotAccessHash is fixed and double-written with the seed row in
	// migration 0153; the two must never drift.
	VerifyBotAccessHash int64 = 7802113947355620887

	// VerifierBotUserID is the built-in @verifierbot: the first THIRD-PARTY
	// verifier of a deployment (core.telegram.org/api/bots/verification). It
	// collects applications for its own icon+description mark and reports the
	// operator's decision back to the applicant. The id is reserved and stable, so
	// a restart never re-creates the account under a different identity.
	//
	// It is not a second route to the platform checkmark: that badge is granted by
	// the operator alone and collected by VerifyBotUserID above.
	VerifierBotUserID int64 = 1250000013
	// VerifierBotAccessHash is fixed and double-written with the seed row in
	// migration 0156; the two must never drift.
	VerifierBotAccessHash int64 = 6913402578811563729
)

// officialSystemUserPhotoDCID/Stripped 由 files.Service.SeedOfficialSystemAvatar
// 在启动时通过 SetOfficialSystemUserAvatar 写入一次；写入前 OfficialSystemUser()
// 不带头像（PhotoID==0），与其它未设置头像的账号行为一致。
var (
	officialSystemUserPhotoDCID     int
	officialSystemUserPhotoStripped []byte
)

// SetOfficialSystemUserAvatar 记录官方系统账号头像所在的 DC 与内联缩略图字节。
// 只应在启动阶段、头像 seed 完成后调用一次。
func SetOfficialSystemUserAvatar(dcID int, stripped []byte) {
	officialSystemUserPhotoDCID = dcID
	officialSystemUserPhotoStripped = stripped
}

// OfficialSystemUser 返回第一阶段内置的官方系统账号。
func OfficialSystemUser() User {
	u := User{
		ID:         OfficialSystemUserID,
		AccessHash: 6599886787491911851,
		Phone:      "42777",
		FirstName:  branding.ProductName,
		Username:   "",
		Verified:   true,
		Support:    true,
	}
	// Service notifications account should appear premium with a purple
	// profile gradient and no direct-call phone number. Emoji status will be
	// resolved at runtime from the seeded default status set; put a placeholder
	// background emoji id for clients that render it.
	u.PremiumUntil = 4102444800 // year 2100
	u.Phone = ""
	u.ProfileColor = PeerColor{
		HasColor:          true,
		Color:             0x6266F1, // purple gradient primary
		BackgroundEmojiID: 900000000000000001,
	}
	if officialSystemUserPhotoDCID != 0 {
		u.PhotoID = OfficialSystemUserPhotoID
		u.PhotoDCID = officialSystemUserPhotoDCID
		u.PhotoStripped = officialSystemUserPhotoStripped
	}
	return u
}

// BotFatherUser 返回内置 BotFather 账号。username 不以 bot 结尾属种子例外（与官方一致）。
func BotFatherUser() User {
	return User{
		ID:             BotFatherUserID,
		AccessHash:     BotFatherAccessHash,
		FirstName:      branding.ProductName + " BotFather",
		Username:       "BotFather",
		Verified:       true,
		Bot:            true,
		BotInfoVersion: 1,
	}
}

// StickersBotUser 返回内置 @Stickers 账号。username 不以 bot 结尾属种子例外（与官方一致）。
func StickersBotUser() User {
	return User{
		ID:             StickersBotUserID,
		AccessHash:     StickersBotAccessHash,
		FirstName:      branding.ProductName + " Stickers",
		Username:       "Stickers",
		Verified:       true,
		Bot:            true,
		BotInfoVersion: 2,
	}
}

// ChatBotUser 返回内置 @ChatBot 账号。
func ChatBotUser() User {
	return User{
		ID:             ChatBotUserID,
		AccessHash:     ChatBotAccessHash,
		FirstName:      branding.ProductName + " ChatBot",
		Username:       "ChatBot",
		Verified:       true,
		Bot:            true,
		BotInfoVersion: 1,
	}
}

// StarsTestBotUser 返回内置 @StarsTestBot 账号。
func StarsTestBotUser() User {
	return User{
		ID:             StarsTestBotUserID,
		AccessHash:     StarsTestBotAccessHash,
		FirstName:      branding.ProductName + " Stars",
		Username:       "StarsTestBot",
		Verified:       true,
		Bot:            true,
		BotInfoVersion: 1,
	}
}

// VerifyBotUser returns the built-in @verifybot account. It is verified itself,
// so the applicant sees the same badge on the account that grants it.
func VerifyBotUser() User {
	return User{
		ID:             VerifyBotUserID,
		AccessHash:     VerifyBotAccessHash,
		FirstName:      "Verify Bot",
		Username:       "verifybot",
		Verified:       true,
		Bot:            true,
		BotInfoVersion: 1,
	}
}

// VerifierBotUser returns the built-in @verifierbot account.
//
// Verified is false on purpose: the official checkmark is the platform's own
// mechanism, and a third-party verifier wearing it would blur exactly the
// distinction this bot has to explain to every applicant. What makes the account a
// verifier is the operator-granted BotVerifierSettings row, not this seed.
func VerifierBotUser() User {
	return User{
		ID:             VerifierBotUserID,
		AccessHash:     VerifierBotAccessHash,
		FirstName:      "Verifier Bot",
		Username:       "verifierbot",
		Verified:       false,
		Bot:            true,
		BotInfoVersion: 1,
	}
}

// SystemUserByID 返回内置系统账号；非系统账号返回 ok=false。
// 所有对 777000 的硬编码注入点统一经此函数，新增内置账号只改这里。
func SystemUserByID(id int64) (User, bool) {
	switch id {
	case OfficialSystemUserID:
		return OfficialSystemUser(), true
	case BotFatherUserID:
		return BotFatherUser(), true
	case StickersBotUserID:
		return StickersBotUser(), true
	case ChatBotUserID:
		return ChatBotUser(), true
	case StarsTestBotUserID:
		return StarsTestBotUser(), true
	case VerifyBotUserID:
		return VerifyBotUser(), true
	case VerifierBotUserID:
		return VerifierBotUser(), true
	}
	return User{}, false
}

func IsSystemUserID(id int64) bool {
	_, ok := SystemUserByID(id)
	return ok
}

// SystemUserIDs returns every built-in account id, in a stable order.
//
// It is the one list to extend when a service account is added, so a caller that
// has to enumerate them -- a SQL predicate excluding infrastructure, say -- cannot
// silently miss one the way an inline literal would.
func SystemUserIDs() []int64 {
	return []int64{
		OfficialSystemUserID,
		BotFatherUserID,
		StickersBotUserID,
		ChatBotUserID,
		StarsTestBotUserID,
		VerifyBotUserID,
		VerifierBotUserID,
	}
}

func SystemUserByPhone(phone string) (User, bool) {
	phone = NormalizePhone(phone)
	for _, id := range SystemUserIDs() {
		u, ok := SystemUserByID(id)
		if !ok || u.Phone == "" {
			continue
		}
		if NormalizePhone(u.Phone) == phone {
			return u, true
		}
	}
	return User{}, false
}
