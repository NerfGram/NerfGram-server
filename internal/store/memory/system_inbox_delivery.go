package memory

import (
	"context"
	"fmt"
	"time"

	"telesrv/internal/domain"
)

// DeliverSystemInboxMessage writes a 777000 inbox message with pts and durable
// update event (same visibility path as login-code delivery).
func (s *LoginCodeDeliveryStore) DeliverSystemInboxMessage(_ context.Context, msg domain.Message) (domain.Message, error) {
	if s == nil || s.messages == nil || s.events == nil {
		return domain.Message{}, fmt.Errorf("memory system inbox delivery: message and update stores are required")
	}
	if msg.OwnerUserID <= 0 || domain.IsSystemUserID(msg.OwnerUserID) {
		return domain.Message{}, fmt.Errorf("memory system inbox delivery: invalid owner %d", msg.OwnerUserID)
	}
	if msg.Peer.Type != domain.PeerTypeUser || msg.Peer.ID != domain.OfficialSystemUserID {
		msg.Peer = domain.Peer{Type: domain.PeerTypeUser, ID: domain.OfficialSystemUserID}
	}
	if msg.From.Type != domain.PeerTypeUser || msg.From.ID != domain.OfficialSystemUserID {
		msg.From = domain.Peer{Type: domain.PeerTypeUser, ID: domain.OfficialSystemUserID}
	}
	if msg.Date == 0 {
		msg.Date = int(time.Now().Unix())
	}

	s.messages.mu.Lock()
	defer s.messages.mu.Unlock()
	s.events.mu.Lock()
	defer s.events.mu.Unlock()

	currentEventPts := 0
	for _, event := range s.events.events[msg.OwnerUserID] {
		if event.Pts > currentEventPts {
			currentEventPts = event.Pts
		}
	}
	if messagePts := s.messages.nextPts[msg.OwnerUserID]; messagePts > currentEventPts {
		return domain.Message{}, fmt.Errorf("memory system inbox delivery: message pts %d exceeds durable event pts %d", messagePts, currentEventPts)
	}

	msg.ID = s.messages.nextBoxIDLocked(msg.OwnerUserID)
	msg.UID = s.messages.nextUID
	s.messages.nextUID++
	msg.Pts = currentEventPts + 1
	msg.Entities = append([]domain.MessageEntity(nil), msg.Entities...)

	s.messages.nextPts[msg.OwnerUserID] = msg.Pts
	s.messages.m[msg.OwnerUserID] = append(s.messages.m[msg.OwnerUserID], cloneMessage(msg))
	if s.messages.dialogs != nil {
		s.messages.dialogs.mu.Lock()
		list := s.messages.dialogs.m[msg.OwnerUserID]
		list = upsertMemoryDialog(list, domain.Dialog{
			Peer:           msg.Peer,
			TopMessage:     msg.ID,
			TopMessageDate: msg.Date,
			UnreadCount:    s.messages.privateUnreadCountLocked(msg.OwnerUserID, msg.Peer),
		})
		if !hasUser(list.Users, domain.OfficialSystemUserID) {
			list.Users = append(list.Users, domain.OfficialSystemUser())
		}
		list.Messages = append(list.Messages, cloneMessage(msg))
		s.messages.dialogs.m[msg.OwnerUserID] = list
		s.messages.dialogs.mu.Unlock()
	}
	event := newMessageEvent(msg)
	s.events.events[msg.OwnerUserID] = append(s.events.events[msg.OwnerUserID], cloneUpdateEvent(event))
	return cloneMessage(msg), nil
}

// Ensure LoginCodeDeliveryStore implements store.SystemInboxDeliveryStore.
var _ interface {
	DeliverSystemInboxMessage(context.Context, domain.Message) (domain.Message, error)
} = (*LoginCodeDeliveryStore)(nil)
