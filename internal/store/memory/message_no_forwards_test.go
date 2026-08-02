package memory

import (
	"context"
	"errors"
	"testing"

	"telesrv/internal/domain"
)

func TestPrivateNoForwardsStateMachineAndForwardGate(t *testing.T) {
	ctx := context.Background()
	messages := NewMessageStore()
	const alice, bob int64 = 1001, 1002

	enable, err := messages.TogglePrivateNoForwards(ctx, domain.TogglePrivateNoForwardsRequest{
		ActorUserID: alice, PeerUserID: bob, Enabled: true, RandomID: 11, Date: 100,
	})
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !enable.Changed || enable.State.EnabledByUserID != alice ||
		enable.Send.SenderMessage.Pts != 1 || enable.Send.RecipientMessage.Pts != 1 ||
		enable.Send.SenderMessage.NoForwards {
		t.Fatalf("enable result = %+v", enable)
	}
	assertMemoryNoForwardsAction(t, enable.Send.SenderMessage, domain.MessageServiceActionNoForwardsToggle, false, true, false)

	repeat, err := messages.TogglePrivateNoForwards(ctx, domain.TogglePrivateNoForwardsRequest{
		ActorUserID: alice, PeerUserID: bob, Enabled: true, RandomID: 12, Date: 101,
	})
	if err != nil || repeat.Changed || repeat.State.EnabledByUserID != alice {
		t.Fatalf("repeat enable = %+v err=%v, want no-op", repeat, err)
	}

	request, err := messages.TogglePrivateNoForwards(ctx, domain.TogglePrivateNoForwardsRequest{
		ActorUserID: bob, PeerUserID: alice, Enabled: false, RandomID: 13, Date: 102,
	})
	if err != nil {
		t.Fatalf("request disable: %v", err)
	}
	if request.State.EnabledByUserID != alice || request.Send.SenderMessage.Pts != 2 ||
		request.Send.RecipientMessage.Pts != 2 {
		t.Fatalf("request result = %+v", request)
	}
	assertMemoryNoForwardsAction(t, request.Send.SenderMessage, domain.MessageServiceActionNoForwardsRequest, true, false, false)

	answer, err := messages.TogglePrivateNoForwards(ctx, domain.TogglePrivateNoForwardsRequest{
		ActorUserID:  alice,
		PeerUserID:   bob,
		Enabled:      false,
		RequestMsgID: request.Send.RecipientMessage.ID,
		RandomID:     14,
		Date:         103,
	})
	if err != nil {
		t.Fatalf("accept request: %v", err)
	}
	if answer.State.Enabled() || answer.Send.SenderMessage.Pts != 3 || answer.Send.RecipientMessage.Pts != 3 {
		t.Fatalf("answer result = %+v", answer)
	}
	if answer.Send.SenderMessage.ReplyTo == nil ||
		answer.Send.SenderMessage.ReplyTo.MessageID != request.Send.RecipientMessage.ID ||
		answer.Send.RecipientMessage.ReplyTo == nil ||
		answer.Send.RecipientMessage.ReplyTo.MessageID != request.Send.SenderMessage.ID {
		t.Fatalf("answer reply mapping sender=%+v recipient=%+v", answer.Send.SenderMessage.ReplyTo, answer.Send.RecipientMessage.ReplyTo)
	}
	assertMemoryNoForwardsAction(t, answer.Send.SenderMessage, domain.MessageServiceActionNoForwardsToggle, true, false, false)

	aliceHistory, err := messages.ListByUser(ctx, alice, domain.MessageFilter{
		HasPeer: true, Peer: domain.Peer{Type: domain.PeerTypeUser, ID: bob}, Limit: 20,
	})
	if err != nil {
		t.Fatalf("alice history: %v", err)
	}
	var expired bool
	for _, msg := range aliceHistory.Messages {
		if msg.ID == request.Send.RecipientMessage.ID && msg.Media != nil && msg.Media.ServiceAction != nil &&
			msg.Media.ServiceAction.NoForwards != nil {
			expired = msg.Media.ServiceAction.NoForwards.Expired
		}
	}
	if !expired {
		t.Fatal("handled request was not projected expired")
	}
	if _, err := messages.TogglePrivateNoForwards(ctx, domain.TogglePrivateNoForwardsRequest{
		ActorUserID: alice, PeerUserID: bob, RequestMsgID: request.Send.RecipientMessage.ID,
		RandomID: 15, Date: 104,
	}); !errors.Is(err, domain.ErrNoForwardsRequestExpired) {
		t.Fatalf("repeat answer err=%v, want ErrNoForwardsRequestExpired", err)
	}

	source, err := messages.SendPrivateText(ctx, domain.SendPrivateTextRequest{
		SenderUserID: alice, RecipientUserID: bob, RandomID: 20, Message: "source", Date: 105,
	})
	if err != nil {
		t.Fatalf("send source: %v", err)
	}
	if _, err := messages.TogglePrivateNoForwards(ctx, domain.TogglePrivateNoForwardsRequest{
		ActorUserID: alice, PeerUserID: bob, Enabled: true, RandomID: 21, Date: 106,
	}); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if _, err := messages.ForwardPrivateMessages(ctx, domain.ForwardPrivateMessagesRequest{
		OwnerUserID: alice,
		FromPeer:    domain.Peer{Type: domain.PeerTypeUser, ID: bob},
		ToUserID:    alice,
		MessageIDs:  []int{source.SenderMessage.ID},
		RandomIDs:   []int64{22},
		Date:        107,
	}); !errors.Is(err, domain.ErrChatForwardsRestricted) {
		t.Fatalf("forward protected chat err=%v, want ErrChatForwardsRestricted", err)
	}
}

func TestPrivateNoForwardsRequestExpiresWithoutPTS(t *testing.T) {
	ctx := context.Background()
	messages := NewMessageStore()
	const alice, bob int64 = 2001, 2002
	if _, err := messages.TogglePrivateNoForwards(ctx, domain.TogglePrivateNoForwardsRequest{
		ActorUserID: alice, PeerUserID: bob, Enabled: true, RandomID: 31, Date: 200,
	}); err != nil {
		t.Fatal(err)
	}
	request, err := messages.TogglePrivateNoForwards(ctx, domain.TogglePrivateNoForwardsRequest{
		ActorUserID: bob, PeerUserID: alice, RandomID: 32, Date: 201,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := messages.TogglePrivateNoForwards(ctx, domain.TogglePrivateNoForwardsRequest{
		ActorUserID:  alice,
		PeerUserID:   bob,
		RequestMsgID: request.Send.RecipientMessage.ID,
		RandomID:     33,
		Date:         201 + domain.PrivateNoForwardsRequestExpirePeriod,
	}); !errors.Is(err, domain.ErrNoForwardsRequestExpired) {
		t.Fatalf("expired answer err=%v", err)
	}
	state, _ := messages.GetPrivateNoForwards(ctx, alice, bob)
	if state.EnabledByUserID != alice {
		t.Fatalf("expired answer changed state = %+v", state)
	}
	history, _ := messages.ListByUser(ctx, alice, domain.MessageFilter{
		HasPeer: true, Peer: domain.Peer{Type: domain.PeerTypeUser, ID: bob}, Limit: 20,
	})
	if len(history.Messages) != 2 || history.Messages[0].Pts != 2 {
		t.Fatalf("expired answer allocated message/pts: %+v", history.Messages)
	}
}

func assertMemoryNoForwardsAction(t *testing.T, msg domain.Message, kind domain.MessageServiceActionKind, prev, next, expired bool) {
	t.Helper()
	if msg.Media == nil || msg.Media.ServiceAction == nil || msg.Media.ServiceAction.Kind != kind ||
		msg.Media.ServiceAction.NoForwards == nil {
		t.Fatalf("message action = %+v, want %s", msg.Media, kind)
	}
	action := msg.Media.ServiceAction.NoForwards
	if action.PrevValue != prev || action.NewValue != next || action.Expired != expired {
		t.Fatalf("action = %+v, want prev=%v new=%v expired=%v", action, prev, next, expired)
	}
}
