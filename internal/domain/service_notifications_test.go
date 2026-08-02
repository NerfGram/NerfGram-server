package domain

import (
	"strings"
	"testing"
	"time"
)

func TestOfficialServiceMessagesUsePremiumEmojiFormat(t *testing.T) {
	welcome, err := OfficialWelcomeMessage(42, "phone number", 1)
	if err != nil {
		t.Fatalf("welcome: %v", err)
	}
	if !strings.Contains(welcome.Body, "Добро пожаловать в FromGram") {
		t.Fatalf("welcome body = %q", welcome.Body)
	}
	assertHasCustomEmoji(t, welcome.Entities, ServiceEmojiParty)
	assertHasCustomEmoji(t, welcome.Entities, ServiceEmojiPerson)

	login, err := OfficialLoginCodeMessage(42, "12345", 1)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if !strings.Contains(login.Body, "FromGram") || !strings.Contains(login.Body, "12345") {
		t.Fatalf("login body = %q", login.Body)
	}
	assertHasCustomEmoji(t, login.Entities, ServiceEmojiLock)
	assertHasCustomEmoji(t, login.Entities, ServiceEmojiWarning)
	assertHasCustomEmoji(t, login.Entities, ServiceEmojiQuestion)
	assertHasType(t, login.Entities, MessageEntitySpoiler)

	when := FormatMoscowLoginWhen(time.Date(2026, 7, 31, 10, 1, 32, 0, time.UTC))
	newLogin, err := OfficialNewLoginMessage(42, "denis", when, "tdesktop, v1.1.3", "The Netherlands", 1)
	if err != nil {
		t.Fatalf("new login: %v", err)
	}
	for _, want := range []string{"Новый вход", "denis", when, "tdesktop", "The Netherlands", "Настройки > Устройства"} {
		if !strings.Contains(newLogin.Body, want) {
			t.Fatalf("new login body %q missing %q", newLogin.Body, want)
		}
	}
	assertHasCustomEmoji(t, newLogin.Entities, ServiceEmojiPerson)
	assertHasCustomEmoji(t, newLogin.Entities, ServiceEmojiLaptop)
	assertHasCustomEmoji(t, newLogin.Entities, ServiceEmojiPin)
	assertHasCustomEmoji(t, newLogin.Entities, ServiceEmojiExclaim)
}

func assertHasCustomEmoji(t *testing.T, entities []MessageEntity, documentID int64) {
	t.Helper()
	for _, entity := range entities {
		if entity.Type == MessageEntityCustomEmoji && entity.DocumentID == documentID && entity.Length > 0 {
			return
		}
	}
	t.Fatalf("missing custom emoji %d in %+v", documentID, entities)
}

func assertHasType(t *testing.T, entities []MessageEntity, typ MessageEntityType) {
	t.Helper()
	for _, entity := range entities {
		if entity.Type == typ && entity.Length > 0 {
			return
		}
	}
	t.Fatalf("missing entity type %s in %+v", typ, entities)
}
