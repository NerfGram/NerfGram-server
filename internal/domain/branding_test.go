package domain

import (
	"strings"
	"testing"
)

func TestServiceIdentityAndLoginMessageUseFromGramBrand(t *testing.T) {
	serviceUser := OfficialSystemUser()
	if serviceUser.FirstName != "FromGram" || serviceUser.Username != "fromgram" {
		t.Fatalf("service user = %+v, want FromGram identity", serviceUser)
	}
	message, err := OfficialLoginCodeMessage(42, "12345", 1)
	if err != nil {
		t.Fatalf("build login message: %v", err)
	}
	if !strings.Contains(message.Body, "FromGram") || strings.Contains(strings.ToLower(message.Body), "telegram") {
		t.Fatalf("login message exposes wrong brand: %q", message.Body)
	}
}
