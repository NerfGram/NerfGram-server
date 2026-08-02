package memory

import (
	"context"
	"fmt"
	"sort"
	"telesrv/internal/domain"
	"time"
)

func (s *MessageStore) DeleteMessages(_ context.Context, req domain.DeleteMessagesRequest) (domain.DeleteMessagesResult, error) {
	res := domain.DeleteMessagesResult{OwnerUserID: req.OwnerUserID}
	ids := normalizeMemoryMessageIDs(req.IDs)
	if req.OwnerUserID == 0 || len(ids) == 0 {
		return res, nil
	}
	if len(ids) > domain.MaxDeleteMessageIDs {
		return res, fmt.Errorf("delete messages: too many ids: %d > %d", len(ids), domain.MaxDeleteMessageIDs)
	}
	if req.Date == 0 {
		req.Date = int(time.Now().Unix())
	}
	idSet := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted, revokeUIDs, _ := s.deleteMemoryMessagesLocked(req.OwnerUserID, 0, func(msg domain.Message) bool {
		_, ok := idSet[msg.ID]
		return ok
	})
	if req.Revoke && len(revokeUIDs) > 0 {
		deleted = append(deleted, s.deleteMemoryMessagesByUIDLocked(revokeUIDs, req.OwnerUserID)...)
	}
	return s.finishMemoryDeleteLocked(res, deleted, req.Date, nil), nil
}

type deletedMemoryMessage struct {
	userID           int64
	peer             domain.Peer
	id               int
	privateMessageID int64
	messageSenderID  int64
	randomID         int64
}

type memoryHistoryClearAnchor struct {
	message      domain.Message
	materialized bool
}

func (s *MessageStore) finishMemoryDeleteLocked(res domain.DeleteMessagesResult, deleted []deletedMemoryMessage, date int, anchors map[int64]memoryHistoryClearAnchor) domain.DeleteMessagesResult {
	if len(deleted) == 0 && len(anchors) == 0 {
		return res
	}
	idsByOwner := make(map[int64][]int)
	peersByOwner := make(map[int64]map[domain.Peer]struct{})
	for _, row := range deleted {
		if byMessage := s.savedMessageTags[row.userID]; byMessage != nil {
			delete(byMessage, row.id)
			if len(byMessage) == 0 {
				delete(s.savedMessageTags, row.userID)
			}
		}
		idsByOwner[row.userID] = append(idsByOwner[row.userID], row.id)
		if peersByOwner[row.userID] == nil {
			peersByOwner[row.userID] = make(map[domain.Peer]struct{})
		}
		peersByOwner[row.userID][row.peer] = struct{}{}
	}
	for userID, anchor := range anchors {
		if peersByOwner[userID] == nil {
			peersByOwner[userID] = make(map[domain.Peer]struct{})
		}
		peersByOwner[userID][anchor.message.Peer] = struct{}{}
	}
	ownerSet := make(map[int64]struct{}, len(idsByOwner)+len(anchors))
	for userID := range idsByOwner {
		ownerSet[userID] = struct{}{}
	}
	for userID, anchor := range anchors {
		if !anchor.materialized {
			ownerSet[userID] = struct{}{}
		}
	}
	ownerIDs := make([]int64, 0, len(ownerSet))
	for userID := range ownerSet {
		ownerIDs = append(ownerIDs, userID)
	}
	sort.Slice(ownerIDs, func(i, j int) bool { return ownerIDs[i] < ownerIDs[j] })
	for _, userID := range ownerIDs {
		ids := normalizeMemoryMessageIDs(idsByOwner[userID])
		anchor, hasAnchor := anchors[userID]
		materializeAnchor := hasAnchor && !anchor.materialized
		totalPtsCount := len(ids)
		if materializeAnchor {
			totalPtsCount += 2
		}
		if totalPtsCount == 0 {
			continue
		}
		pts := s.nextPtsNLocked(userID, totalPtsCount)
		cursor := pts - totalPtsCount
		item := domain.DeletedMessagesForUser{
			UserID:     userID,
			MessageIDs: ids,
			Pts:        pts,
			PtsCount:   totalPtsCount,
			Events:     make([]domain.UpdateEvent, 0, 3),
		}
		if len(ids) > 0 {
			cursor += len(ids)
			event := domain.UpdateEvent{
				UserID:     userID,
				Type:       domain.UpdateEventDeleteMessages,
				Pts:        cursor,
				PtsCount:   len(ids),
				Date:       date,
				MessageIDs: ids,
			}
			for _, row := range deleted {
				if row.userID != userID || row.messageSenderID != userID || row.randomID == 0 || row.privateMessageID == 0 {
					continue
				}
				key := privateSendDedupKey{senderUserID: userID, randomID: row.randomID}
				record, ok := s.privateSendDedup[key]
				if !ok {
					continue
				}
				cloned := cloneUpdateEvent(event)
				record.senderDeleteEvent = &cloned
				s.privateSendDedup[key] = record
			}
			item.Event = event
			item.Events = append(item.Events, event)
		}
		if materializeAnchor {
			readPts := cursor + 1
			editPts := readPts + 1
			msg := domain.NewHistoryClearMessage(
				userID,
				anchor.message.Peer,
				anchor.message.ID,
				anchor.message.UID,
				anchor.message.Date,
				editPts,
			)
			for i := range s.m[userID] {
				if s.m[userID][i].ID == anchor.message.ID && s.m[userID][i].Peer == anchor.message.Peer {
					s.m[userID][i] = msg
					break
				}
			}
			if byMessage := s.savedMessageTags[userID]; byMessage != nil {
				delete(byMessage, anchor.message.ID)
				if len(byMessage) == 0 {
					delete(s.savedMessageTags, userID)
				}
			}
			readEvent := domain.UpdateEvent{
				UserID:           userID,
				Type:             domain.UpdateEventReadHistoryInbox,
				Pts:              readPts,
				PtsCount:         1,
				Date:             date,
				Peer:             anchor.message.Peer,
				MaxID:            anchor.message.ID,
				StillUnreadCount: 0,
			}
			editEvent := domain.UpdateEvent{
				UserID:   userID,
				Type:     domain.UpdateEventEditMessage,
				Pts:      editPts,
				PtsCount: 1,
				Date:     date,
				Message:  cloneMessage(msg),
			}
			item.Events = append(item.Events, readEvent, editEvent)
			cursor = editPts
		}
		if s.dialogs != nil {
			s.dialogs.mu.Lock()
			for peer := range peersByOwner[userID] {
				s.rebuildMemoryDialogLocked(userID, peer)
			}
			if materializeAnchor {
				s.advanceMemoryHistoryClearDialogLocked(userID, anchor.message.Peer, anchor.message.ID)
			}
			s.dialogs.mu.Unlock()
		}
		if cursor != pts {
			panic(fmt.Sprintf("memory delete history pts cursor %d does not reach reserved pts %d", cursor, pts))
		}
		res.Deleted = append(res.Deleted, item)
	}
	return res
}

func (s *MessageStore) advanceMemoryHistoryClearDialogLocked(userID int64, peer domain.Peer, maxID int) {
	list := s.dialogs.m[userID]
	for i := range list.Dialogs {
		if list.Dialogs[i].Peer != peer {
			continue
		}
		if list.Dialogs[i].ReadInboxMaxID < maxID {
			list.Dialogs[i].ReadInboxMaxID = maxID
		}
		list.Dialogs[i].UnreadCount = 0
		list.Dialogs[i].UnreadMark = false
		list.Dialogs[i].UnreadMentions = 0
		list.Dialogs[i].UnreadReactions = 0
		break
	}
	s.dialogs.m[userID] = list
}

func (s *MessageStore) rebuildMemoryDialogLocked(userID int64, peer domain.Peer) {
	list := s.dialogs.m[userID]
	topID := 0
	topDate := 0
	unread := 0
	for _, msg := range s.m[userID] {
		if msg.Peer != peer {
			continue
		}
		if msg.ID > topID {
			topID = msg.ID
			topDate = msg.Date
		}
	}
	dialogs := list.Dialogs[:0]
	for _, dialog := range list.Dialogs {
		if dialog.Peer != peer {
			dialogs = append(dialogs, dialog)
			continue
		}
		if topID == 0 {
			continue
		}
		for _, msg := range s.m[userID] {
			if msg.Peer == peer && !msg.Out && msg.ID > dialog.ReadInboxMaxID {
				unread++
			}
		}
		dialog.TopMessage = topID
		dialog.TopMessageDate = topDate
		dialog.UnreadCount = unread
		dialog.UnreadMentions = 0
		dialog.UnreadReactions = 0
		dialogs = append(dialogs, dialog)
	}
	list.Dialogs = dialogs
	list.Messages = cloneMessages(s.m[userID])
	s.dialogs.m[userID] = list
}
