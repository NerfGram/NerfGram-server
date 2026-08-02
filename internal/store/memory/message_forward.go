package memory

import (
	"context"
	"telesrv/internal/domain"
	"time"
)

func (s *MessageStore) ForwardPrivateMessages(ctx context.Context, req domain.ForwardPrivateMessagesRequest) (domain.ForwardPrivateMessagesResult, error) {
	res := domain.ForwardPrivateMessagesResult{OwnerUserID: req.OwnerUserID}
	if req.OwnerUserID == 0 || req.ToUserID == 0 || req.FromPeer.Type != domain.PeerTypeUser || req.FromPeer.ID == 0 {
		return res, domain.ErrMessageIDInvalid
	}
	if len(req.MessageIDs) == 0 || len(req.MessageIDs) != len(req.RandomIDs) {
		return res, domain.ErrMessageIDInvalid
	}
	if len(req.MessageIDs) > domain.MaxForwardMessageIDs {
		return res, domain.ErrMessageIDInvalid
	}
	if req.Date == 0 {
		req.Date = int(time.Now().Unix())
	}
	if s.privateNoForwardsEnabled(req.OwnerUserID, req.FromPeer.ID) {
		return res, domain.ErrChatForwardsRestricted
	}
	s.mu.RLock()
	sources := make([]domain.Message, 0, len(req.MessageIDs))
	for _, id := range req.MessageIDs {
		if id <= 0 || id > domain.MaxMessageBoxID {
			s.mu.RUnlock()
			return res, domain.ErrMessageIDInvalid
		}
		var source domain.Message
		for _, msg := range s.m[req.OwnerUserID] {
			if msg.Peer == req.FromPeer && msg.ID == id {
				source = cloneMessage(msg)
				break
			}
		}
		if source.ID == 0 {
			s.mu.RUnlock()
			return res, domain.ErrMessageIDInvalid
		}
		if source.NoForwards {
			s.mu.RUnlock()
			return res, domain.ErrChatForwardsRestricted
		}
		sources = append(sources, source)
	}
	s.mu.RUnlock()

	res.SenderMessages = make([]domain.Message, 0, len(sources))
	res.RecipientMessages = make([]domain.Message, 0, len(sources))
	res.SenderEvents = make([]domain.UpdateEvent, 0, len(sources))
	res.RecipientEvents = make([]domain.UpdateEvent, 0, len(sources))
	res.Duplicates = make([]bool, 0, len(sources))
	for i, source := range sources {
		if req.RandomIDs[i] == 0 {
			return res, domain.ErrMessageIDInvalid
		}
		var forward *domain.MessageForward
		if !req.DropAuthor {
			forward = cloneMessageForward(source.Forward)
			if forward == nil {
				forward = &domain.MessageForward{From: source.From, Date: source.Date}
			}
			if req.ToUserID == req.OwnerUserID {
				forward.SavedFrom = req.FromPeer
				forward.SavedFromMsgID = req.MessageIDs[i]
			}
		}
		sent, err := s.SendPrivateText(ctx, domain.SendPrivateTextRequest{
			SenderUserID:     req.OwnerUserID,
			RecipientUserID:  req.ToUserID,
			RandomID:         req.RandomIDs[i],
			Message:          source.Body,
			Entities:         append([]domain.MessageEntity(nil), source.Entities...),
			Media:            source.Media,
			Silent:           req.Silent,
			NoForwards:       req.NoForwards,
			ReplyTo:          req.ReplyTo,
			Forward:          forward,
			Date:             req.Date,
			OriginAuthKeyID:  req.OriginAuthKeyID,
			OriginSessionID:  req.OriginSessionID,
			RecipientBlocked: req.RecipientBlocked,
		})
		if err != nil {
			return res, err
		}
		res.SenderMessages = append(res.SenderMessages, sent.SenderMessage)
		res.RecipientMessages = append(res.RecipientMessages, sent.RecipientMessage)
		res.SenderEvents = append(res.SenderEvents, sent.SenderEvent)
		res.RecipientEvents = append(res.RecipientEvents, sent.RecipientEvent)
		res.Duplicates = append(res.Duplicates, sent.Duplicate)
	}
	return res, nil
}
