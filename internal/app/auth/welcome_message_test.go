package auth

import (
	"context"
	"strings"
	"testing"

	"telesrv/internal/domain"
	"telesrv/internal/store/memory"
)

func TestEmailSignupSignUpWritesWelcomeMessageOnce(t *testing.T) {
	ctx := context.Background()
	dialogs := memory.NewDialogStore()
	messages := memory.NewMessageStore(dialogs)
	sender := &testMailSender{}
	svc := NewService(memory.NewUserStore(), memory.NewAuthorizationStore(), memory.NewCodeStore(), nil, nil, "12345",
		WithLoginMessages(messages, dialogs),
		WithLoginEmail(LoginEmailOptions{Sender: sender}),
		WithEmailSignup(true))

	phone, ok := domain.EncodeEmailPhone("welcome@fromgram.local")
	if !ok {
		t.Fatalf("EncodeEmailPhone: ok=false")
	}
	hash, err := svc.SendCode(ctx, phone)
	if err != nil {
		t.Fatalf("SendCode: %v", err)
	}
	if _, _, needSignUp, err := svc.SignInWithEmail(ctx, domain.Authorization{}, phone, hash, sender.code); err != nil || !needSignUp {
		t.Fatalf("SignInWithEmail: needSignUp=%v err=%v", needSignUp, err)
	}
	u, _, err := svc.SignUp(ctx, domain.Authorization{}, phone, hash, "Welcome", "User")
	if err != nil {
		t.Fatalf("SignUp: %v", err)
	}

	list, err := dialogs.ListByUser(ctx, u.ID, domain.DialogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list.Messages) != 1 {
		t.Fatalf("messages = %+v, want exactly the welcome message (email channel skips the code-echo message)", list.Messages)
	}
	if !strings.Contains(list.Messages[0].Body, "Добро пожаловать в FromGram") {
		t.Fatalf("welcome message body = %q, want Russian FromGram welcome", list.Messages[0].Body)
	}
}

func TestSignInWritesNewLoginMessageNotWelcome(t *testing.T) {
	ctx := context.Background()
	dialogs := memory.NewDialogStore()
	messages := memory.NewMessageStore(dialogs)
	delivery := memory.NewLoginCodeDeliveryStore(messages, memory.NewUpdateEventStore())
	svc := NewService(memory.NewUserStore(), memory.NewAuthorizationStore(), memory.NewCodeStore(), nil, nil, "12345",
		WithLoginMessages(messages, dialogs),
		WithLoginCodeDelivery(delivery),
	)
	var key [8]byte
	key[0] = 42

	hash, err := svc.SendCode(ctx, "+15550009911")
	if err != nil {
		t.Fatalf("SendCode signup: %v", err)
	}
	verifyCodeForSignUp(t, svc, "+15550009911", hash, "12345")
	u, _, err := svc.SignUp(ctx, domain.Authorization{AuthKeyID: key}, "+15550009911", hash, "Repeat", "Login")
	if err != nil {
		t.Fatalf("SignUp: %v", err)
	}
	afterSignUp, err := dialogs.ListByUser(ctx, u.ID, domain.DialogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListByUser after signup: %v", err)
	}
	if len(afterSignUp.Messages) != 1 || !strings.Contains(afterSignUp.Messages[0].Body, "Добро пожаловать в FromGram") {
		t.Fatalf("after signup top = %+v, want welcome", afterSignUp.Messages)
	}
	if err := svc.LogOut(ctx, key); err != nil {
		t.Fatalf("LogOut: %v", err)
	}

	hash, err = svc.SendCode(ctx, "+15550009911")
	if err != nil {
		t.Fatalf("SendCode signin: %v", err)
	}
	var key2 [8]byte
	key2[0] = 43
	code := loginCodeFromOfficialTopMessage(t, dialogs, u.ID)
	if _, _, _, err := svc.SignIn(ctx, domain.Authorization{AuthKeyID: key2}, "+15550009911", hash, code); err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	afterSignIn, err := dialogs.ListByUser(ctx, u.ID, domain.DialogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if strings.Contains(afterSignIn.Messages[0].Body, "Добро пожаловать в FromGram") {
		t.Fatalf("second sign-in top message should not be a new welcome: %q", afterSignIn.Messages[0].Body)
	}

	svc.RecordNewLoginMessage(ctx, u, "сейчас", "testdevice", "Testland")
	list, err := dialogs.ListByUser(ctx, u.ID, domain.DialogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListByUser after new-login: %v", err)
	}
	if !strings.Contains(list.Messages[0].Body, "Новый вход") || !strings.Contains(list.Messages[0].Body, "testdevice") {
		t.Fatalf("new login message = %q, want Russian new-login with device", list.Messages[0].Body)
	}
}

func TestTwoFactorSignInDoesNotSendWelcomeOnPasswordComplete(t *testing.T) {
	ctx := context.Background()
	passwords := memory.NewPasswordStore()
	dialogs := memory.NewDialogStore()
	messages := memory.NewMessageStore(dialogs)
	delivery := memory.NewLoginCodeDeliveryStore(messages, memory.NewUpdateEventStore())
	svc := NewService(memory.NewUserStore(), memory.NewAuthorizationStore(), memory.NewCodeStore(), nil, nil, "12345",
		WithPasswords(passwords),
		WithLoginMessages(messages, dialogs),
		WithLoginCodeDelivery(delivery),
	)
	var key [8]byte
	key[0] = 9

	hash, err := svc.SendCode(ctx, "+15550009922")
	if err != nil {
		t.Fatalf("SendCode signup: %v", err)
	}
	verifyCodeForSignUp(t, svc, "+15550009922", hash, "12345")
	u, _, err := svc.SignUp(ctx, domain.Authorization{AuthKeyID: key}, "+15550009922", hash, "Two", "Factor")
	if err != nil {
		t.Fatalf("SignUp: %v", err)
	}
	afterSignUp, err := dialogs.ListByUser(ctx, u.ID, domain.DialogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListByUser after signup: %v", err)
	}
	if !strings.Contains(afterSignUp.Messages[0].Body, "Добро пожаловать в FromGram") {
		t.Fatalf("signup top = %q, want welcome", afterSignUp.Messages[0].Body)
	}
	welcomeTopID := afterSignUp.Messages[0].ID
	if err := svc.LogOut(ctx, key); err != nil {
		t.Fatalf("LogOut: %v", err)
	}
	if err := passwords.Save(ctx, u.ID, domain.PasswordSettings{HasPassword: true}); err != nil {
		t.Fatalf("save password settings: %v", err)
	}

	hash, err = svc.SendCode(ctx, "+15550009922")
	if err != nil {
		t.Fatalf("SendCode signin: %v", err)
	}
	code := loginCodeFromOfficialTopMessage(t, dialogs, u.ID)
	if _, _, _, err := svc.SignIn(ctx, domain.Authorization{AuthKeyID: key}, "+15550009922", hash, code); err == nil {
		t.Fatalf("SignIn err = nil, want ErrSessionPasswordNeeded")
	}

	pending, err := dialogs.ListByUser(ctx, u.ID, domain.DialogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListByUser pending: %v", err)
	}
	if strings.Contains(pending.Messages[0].Body, "Новый вход") {
		t.Fatalf("new-login message fired before password check completed: %+v", pending.Messages[0])
	}

	if err := svc.CompletePasswordSignIn(ctx, key); err != nil {
		t.Fatalf("CompletePasswordSignIn: %v", err)
	}

	done, err := dialogs.ListByUser(ctx, u.ID, domain.DialogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListByUser done: %v", err)
	}
	if done.Messages[0].ID == welcomeTopID && strings.Contains(done.Messages[0].Body, "Добро пожаловать в FromGram") {
		// Still only the signup welcome as historical top if no newer inbox rows — OK
		// as long as CompletePasswordSignIn did not append another welcome.
		return
	}
	if strings.Contains(done.Messages[0].Body, "Добро пожаловать в FromGram") && done.Messages[0].ID != welcomeTopID {
		t.Fatalf("CompletePasswordSignIn wrote a second welcome: %+v", done.Messages[0])
	}
}
