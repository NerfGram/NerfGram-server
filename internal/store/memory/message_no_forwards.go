package memory

import (
	"context"
	"time"

	"telesrv/internal/domain"
)

type privateNoForwardsPair struct {
	low  int64
	high int64
}

type memoryNoForwardsRequest struct {
	privateMessageID int64
	requesterUserID  int64
	responderUserID  int64
	expiresAt        int
	handled          bool
}

func noForwardsPair(a, b int64) (privateNoForwardsPair, bool) {
	if a <= 0 || b <= 0 || a == b {
		return privateNoForwardsPair{}, false
	}
	if a > b {
		a, b = b, a
	}
	return privateNoForwardsPair{low: a, high: b}, true
}

func (s *MessageStore) GetPrivateNoForwards(_ context.Context, viewerUserID, peerUserID int64) (domain.PrivateNoForwardsState, error) {
	pair, ok := noForwardsPair(viewerUserID, peerUserID)
	if !ok {
		return domain.PrivateNoForwardsState{}, domain.ErrMessageIDInvalid
	}
	s.noForwardsMu.Lock()
	defer s.noForwardsMu.Unlock()
	state := s.privateNoForwards[pair]
	state.UserLowID, state.UserHighID = pair.low, pair.high
	return state, nil
}

func (s *MessageStore) TogglePrivateNoForwards(ctx context.Context, req domain.TogglePrivateNoForwardsRequest) (domain.TogglePrivateNoForwardsResult, error) {
	pair, ok := noForwardsPair(req.ActorUserID, req.PeerUserID)
	if !ok || req.RequestMsgID < 0 || req.RequestMsgID > domain.MaxMessageBoxID {
		return domain.TogglePrivateNoForwardsResult{}, domain.ErrMessageIDInvalid
	}
	if req.Date == 0 {
		req.Date = int(time.Now().Unix())
	}
	if req.RandomID == 0 {
		req.RandomID = time.Now().UnixNano()
		if req.RandomID == 0 {
			req.RandomID = 1
		}
	}

	s.noForwardsMu.Lock()
	defer s.noForwardsMu.Unlock()

	state := s.privateNoForwards[pair]
	state.UserLowID, state.UserHighID = pair.low, pair.high
	previousEnabled := state.Enabled()
	var (
		kind          domain.MessageServiceActionKind
		action        domain.MessageNoForwardsAction
		requestRecord *memoryNoForwardsRequest
		requestUID    int64
	)

	if req.RequestMsgID != 0 {
		s.mu.RLock()
		var source domain.Message
		for _, msg := range s.m[req.ActorUserID] {
			if msg.ID == req.RequestMsgID && msg.Peer == (domain.Peer{Type: domain.PeerTypeUser, ID: req.PeerUserID}) {
				source = msg
				break
			}
		}
		if source.ID != 0 {
			record := s.privateNoForwardsRequests[source.UID]
			requestRecord = &record
			requestUID = source.UID
		}
		s.mu.RUnlock()
		if source.ID == 0 || requestRecord == nil || requestRecord.privateMessageID != source.UID ||
			requestRecord.requesterUserID != req.PeerUserID || requestRecord.responderUserID != req.ActorUserID ||
			requestRecord.handled || requestRecord.expiresAt <= req.Date {
			return domain.TogglePrivateNoForwardsResult{}, domain.ErrNoForwardsRequestExpired
		}
		kind = domain.MessageServiceActionNoForwardsToggle
		action = domain.MessageNoForwardsAction{PrevValue: previousEnabled, NewValue: req.Enabled}
		if req.Enabled {
			state.EnabledByUserID = req.ActorUserID
		} else {
			state.EnabledByUserID = 0
		}
	} else if req.Enabled {
		if state.EnabledByUserID != 0 {
			return domain.TogglePrivateNoForwardsResult{State: state}, nil
		}
		kind = domain.MessageServiceActionNoForwardsToggle
		action = domain.MessageNoForwardsAction{PrevValue: false, NewValue: true}
		state.EnabledByUserID = req.ActorUserID
	} else {
		switch state.EnabledByUserID {
		case 0:
			return domain.TogglePrivateNoForwardsResult{State: state}, nil
		case req.ActorUserID:
			kind = domain.MessageServiceActionNoForwardsToggle
			action = domain.MessageNoForwardsAction{PrevValue: true, NewValue: false}
			state.EnabledByUserID = 0
		default:
			kind = domain.MessageServiceActionNoForwardsRequest
			action = domain.MessageNoForwardsAction{
				PrevValue: true,
				NewValue:  false,
				ExpiresAt: req.Date + domain.PrivateNoForwardsRequestExpirePeriod,
			}
		}
	}

	reply := (*domain.MessageReply)(nil)
	if req.RequestMsgID != 0 {
		reply = &domain.MessageReply{
			MessageID: req.RequestMsgID,
			Peer:      domain.Peer{Type: domain.PeerTypeUser, ID: req.PeerUserID},
		}
	}
	send, err := s.SendPrivateText(ctx, domain.SendPrivateTextRequest{
		SenderUserID:    req.ActorUserID,
		RecipientUserID: req.PeerUserID,
		RandomID:        req.RandomID,
		Silent:          true,
		Date:            req.Date,
		OriginAuthKeyID: req.OriginAuthKeyID,
		OriginSessionID: req.OriginSessionID,
		ReplyTo:         reply,
		Media: &domain.MessageMedia{
			Kind: domain.MessageMediaKindService,
			ServiceAction: &domain.MessageServiceAction{
				Kind:       kind,
				NoForwards: &action,
			},
		},
	})
	if err != nil {
		if req.RequestMsgID != 0 && err == domain.ErrReplyMessageIDInvalid {
			return domain.TogglePrivateNoForwardsResult{}, domain.ErrNoForwardsRequestExpired
		}
		return domain.TogglePrivateNoForwardsResult{}, err
	}

	s.privateNoForwards[pair] = state
	if kind == domain.MessageServiceActionNoForwardsRequest {
		s.privateNoForwardsRequests[send.SenderMessage.UID] = memoryNoForwardsRequest{
			privateMessageID: send.SenderMessage.UID,
			requesterUserID:  req.ActorUserID,
			responderUserID:  req.PeerUserID,
			expiresAt:        action.ExpiresAt,
		}
	}
	if requestUID != 0 {
		record := s.privateNoForwardsRequests[requestUID]
		record.handled = true
		s.privateNoForwardsRequests[requestUID] = record
		s.markNoForwardsRequestExpired(requestUID)
	}
	return domain.TogglePrivateNoForwardsResult{State: state, Changed: true, Send: send}, nil
}

func (s *MessageStore) markNoForwardsRequestExpired(privateMessageID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ownerID, messages := range s.m {
		for i := range messages {
			action := messages[i].Media
			if messages[i].UID != privateMessageID || action == nil || action.ServiceAction == nil ||
				action.ServiceAction.Kind != domain.MessageServiceActionNoForwardsRequest ||
				action.ServiceAction.NoForwards == nil {
				continue
			}
			messages[i].Media = cloneRequestedPeerMedia(messages[i].Media)
			messages[i].Media.ServiceAction.NoForwards.Expired = true
		}
		s.m[ownerID] = messages
	}
}

func (s *MessageStore) privateNoForwardsEnabled(a, b int64) bool {
	pair, ok := noForwardsPair(a, b)
	if !ok {
		return false
	}
	s.noForwardsMu.Lock()
	defer s.noForwardsMu.Unlock()
	return s.privateNoForwards[pair].Enabled()
}
