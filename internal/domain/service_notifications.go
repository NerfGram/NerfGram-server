package domain

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf16"

	"telesrv/internal/branding"
)

// Premium custom-emoji document IDs used by official 777000 service messages.
// These must exist in the documents table (seeded sticker/emoji packs).
const (
	ServiceEmojiParty    int64 = 5436040291507247633
	ServiceEmojiPerson   int64 = 6032994772321309200
	ServiceEmojiLock     int64 = 5778570255555105942
	ServiceEmojiWarning  int64 = 5449405744401128721
	ServiceEmojiQuestion int64 = 6030848053177486888
	ServiceEmojiLaptop   int64 = 5431376038628171216
	ServiceEmojiPin      int64 = 5449676924341232533
	ServiceEmojiExclaim  int64 = 6030563507299160824
)

const (
	officialWelcomeMessageTemplate = "🎉 Добро пожаловать в FromGram!\n\n👤 Вы вошли в аккаунт первый раз. Пожалуйста, не выходите из аккаунта, так как код в следующий раз придет только в этот чат."

	officialNewLoginMessageTemplate = "👤 Новый вход. %s, кто-то вошел в ваш аккаунт %s.\n\n💻 Устройство: %s\n📍 Местоположение: %s\n\n❗️ Если это были не вы, срочно зайдите в Настройки > Устройства и завершите эту сессию."
)

func officialLoginCodeMessageTemplate() string {
	return "🔒 Код для входа: %s.\n\n⚠️ Не говорите этот код никому, даже если его просят от имени " + branding.ProductName() + "!\n\n❓ Если вы не запрашивали этот код для входа на другом устройстве, просто не обращайте внимание на это сообщение."
}

// OfficialWelcomeMessage builds the once-per-account welcome from 777000.
// Callers must send it only on SignUp (first authorization), never on SignIn.
func OfficialWelcomeMessage(userID int64, _ string, date int) (Message, error) {
	if userID <= 0 || IsSystemUserID(userID) || date < 0 || date > math.MaxInt32 {
		return Message{}, fmt.Errorf("%w: user=%d date=%d", ErrLoginCodeDeliveryInvalid, userID, date)
	}
	body := officialWelcomeMessageTemplate
	entities := []MessageEntity{
		mustCustomEmoji(body, "🎉", ServiceEmojiParty),
		mustBold(body, "Добро пожаловать в FromGram!"),
		mustCustomEmoji(body, "👤", ServiceEmojiPerson),
	}
	return Message{
		OwnerUserID: userID,
		Peer:        Peer{Type: PeerTypeUser, ID: OfficialSystemUserID},
		From:        Peer{Type: PeerTypeUser, ID: OfficialSystemUserID},
		Date:        date,
		Body:        body,
		Entities:    entities,
	}, nil
}

// OfficialLoginCodeMessage builds the account-visible login-code message from
// 777000 using the branded Russian template and premium custom emoji.
func OfficialLoginCodeMessage(userID int64, code string, date int) (Message, error) {
	code = strings.TrimSpace(code)
	if userID <= 0 || IsSystemUserID(userID) || code == "" || len(code) > 64 || date < 0 || date > math.MaxInt32 {
		return Message{}, fmt.Errorf("%w: user=%d code_length=%d date=%d", ErrLoginCodeDeliveryInvalid, userID, len(code), date)
	}
	body := fmt.Sprintf(officialLoginCodeMessageTemplate(), code)
	entities := []MessageEntity{
		mustCustomEmoji(body, "🔒", ServiceEmojiLock),
		mustBold(body, "Код для входа:"),
		mustSpoiler(body, code),
		mustCustomEmoji(body, "⚠️", ServiceEmojiWarning),
		mustCustomEmoji(body, "❓", ServiceEmojiQuestion),
	}
	return Message{
		OwnerUserID: userID,
		Peer:        Peer{Type: PeerTypeUser, ID: OfficialSystemUserID},
		From:        Peer{Type: PeerTypeUser, ID: OfficialSystemUserID},
		Date:        date,
		Body:        body,
		Entities:    entities,
	}, nil
}

// OfficialNewLoginMessage builds the 777000 "new login" inbox message sent on
// each completed SignIn after the account already exists.
func OfficialNewLoginMessage(userID int64, displayName, whenLabel, device, location string, date int) (Message, error) {
	displayName = strings.TrimSpace(displayName)
	whenLabel = strings.TrimSpace(whenLabel)
	device = strings.TrimSpace(device)
	location = strings.TrimSpace(location)
	if displayName == "" {
		displayName = "пользователь"
	}
	if whenLabel == "" {
		whenLabel = "только что"
	}
	if device == "" {
		device = "неизвестно"
	}
	if location == "" {
		location = "неизвестно"
	}
	if userID <= 0 || IsSystemUserID(userID) || date < 0 || date > math.MaxInt32 {
		return Message{}, fmt.Errorf("%w: user=%d date=%d", ErrLoginCodeDeliveryInvalid, userID, date)
	}
	body := fmt.Sprintf(officialNewLoginMessageTemplate, displayName, whenLabel, device, location)
	entities := []MessageEntity{
		mustCustomEmoji(body, "👤", ServiceEmojiPerson),
		mustBold(body, "Новый вход."),
		mustCustomEmoji(body, "💻", ServiceEmojiLaptop),
		mustBold(body, "Устройство:"),
		mustCustomEmoji(body, "📍", ServiceEmojiPin),
		mustBold(body, "Местоположение:"),
		mustCustomEmoji(body, "❗️", ServiceEmojiExclaim),
	}
	return Message{
		OwnerUserID: userID,
		Peer:        Peer{Type: PeerTypeUser, ID: OfficialSystemUserID},
		From:        Peer{Type: PeerTypeUser, ID: OfficialSystemUserID},
		Date:        date,
		Body:        body,
		Entities:    entities,
	}, nil
}

// SignInMethodLabel is retained for callers that still label signup channel;
// welcome copy no longer embeds it.
func SignInMethodLabel(u User) string {
	if u.SignupEmail != "" {
		return "email"
	}
	return "phone number"
}

// ServiceNotificationDisplayName picks the account label used in new-login copy.
func ServiceNotificationDisplayName(u User) string {
	name := strings.TrimSpace(strings.TrimSpace(u.FirstName + " " + u.LastName))
	if name != "" {
		return name
	}
	if u.Username != "" {
		return u.Username
	}
	if u.Phone != "" {
		return u.Phone
	}
	return "пользователь"
}

// FormatMoscowLoginWhen formats t as "31 июля 13:01:32 по Москве".
func FormatMoscowLoginWhen(t time.Time) string {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.FixedZone("MSK", 3*3600)
	}
	t = t.In(loc)
	months := [...]string{
		"января", "февраля", "марта", "апреля", "мая", "июня",
		"июля", "августа", "сентября", "октября", "ноября", "декабря",
	}
	return fmt.Sprintf("%d %s %02d:%02d:%02d по Москве",
		t.Day(), months[t.Month()-1], t.Hour(), t.Minute(), t.Second())
}

// FormatLoginDeviceLabel builds a compact device string for new-login notices.
func FormatLoginDeviceLabel(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(branding.UserVisibleText(part, ""))
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	if len(out) == 0 {
		return "неизвестно"
	}
	return strings.Join(out, ", ")
}

func mustCustomEmoji(body, needle string, documentID int64) MessageEntity {
	off, length, ok := utf16Index(body, needle)
	if !ok {
		return MessageEntity{Type: MessageEntityCustomEmoji, DocumentID: documentID}
	}
	return MessageEntity{Type: MessageEntityCustomEmoji, Offset: off, Length: length, DocumentID: documentID}
}

func mustBold(body, needle string) MessageEntity {
	off, length, ok := utf16Index(body, needle)
	if !ok {
		return MessageEntity{Type: MessageEntityBold}
	}
	return MessageEntity{Type: MessageEntityBold, Offset: off, Length: length}
}

func mustSpoiler(body, needle string) MessageEntity {
	off, length, ok := utf16Index(body, needle)
	if !ok {
		return MessageEntity{Type: MessageEntitySpoiler}
	}
	return MessageEntity{Type: MessageEntitySpoiler, Offset: off, Length: length}
}

func utf16Index(s, sub string) (offset, length int, ok bool) {
	idx := strings.Index(s, sub)
	if idx < 0 {
		return 0, 0, false
	}
	return utf16Count(s[:idx]), utf16Count(sub), true
}

func utf16Count(s string) int {
	return len(utf16.Encode([]rune(s)))
}
