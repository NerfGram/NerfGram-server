package mtprotoedge

import (
	"container/list"
	"context"
	"encoding/binary"
	"hash/maphash"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// A valid client msg_id can be up to five minutes old or thirty seconds in
	// the future. The extra second covers scheduler and boundary jitter. This is
	// a no-ACK execution-receipt horizon, not a payload retention policy.
	rpcExecutionReceiptTTL = 331 * time.Second

	rpcExecutionMaxEntries        = 1 << 18
	rpcExecutionAuthMaxEntries    = 1 << 15
	rpcExecutionSessionMaxEntries = 1 << 14
	rpcExecutionPendingPerAuth    = 1 << 11

	// Receipts contain only fixed-shape identity/outcome metadata. This
	// conservative charge covers the receipt, list node, map bucket share and
	// reservation bookkeeping. Payload bytes are accounted exclusively by the
	// logical-session outbox.
	rpcExecutionReceiptBudgetBytes = 384

	// The complete replay identity is hashed with an instance-random seed. The
	// shard count is a power of two.
	rpcExecutionLedgerShards = 16
)

type rpcExecutionKey struct {
	authKeyID [8]byte
	sessionID int64
	reqMsgID  int64
}

// rpcReplayStore is the sole source of retained rpc_result payloads. The
// production implementation is SessionManager's logical-session outbox.
type rpcReplayStore interface {
	rpcResult(authKeyID [8]byte, sessionID, reqMsgID int64) (*encodedOutboundMessage, bool)
}

type rpcExecutionReceipt struct {
	key            rpcExecutionKey
	expiresAt      time.Time
	identity       rpcResultRequestIdentity
	admissionSeq   uint64
	executionKnown bool
	executionOK    bool
	// acknowledged exists only for the ACK-before-Complete race. A completed
	// receipt is removed immediately when the ACK wins.
	acknowledged bool
	// unavailable is a bounded execution tombstone. It prevents a completed
	// business operation from running again when no exact outbox frame exists.
	unavailable bool
	// The pending owner transfers this same global/auth/session entry
	// reservation to the completed receipt.
	reservation *rpcExecutionBudgetReservation
}

type rpcResultDependency struct {
	waiter    *rpcResultWaiter
	completed bool
	success   bool
}

// rpcExecutionLedger owns request execution identity and duplicate
// coordination. It never owns or copies rpc_result payload bytes; replay bytes
// are resolved from replayStore only while the logical-session outbox retains
// the exact unacknowledged frame.
type rpcExecutionLedger struct {
	shards              [rpcExecutionLedgerShards]rpcExecutionLedgerShard
	hashSeed            maphash.Seed
	reservedEntries     rpcResultFlightLimit
	receiptCount        atomic.Int64
	fairBudget          *rpcExecutionFairBudget
	flightLimit         rpcResultFlightLimit
	subscriberBudget    *rpcResultSubscriberBudget
	subscriberPerFlight int
	replayStore         rpcReplayStore
	nextAdmissionSeq    atomic.Uint64
	activeAdmissions    rpcAdmissionTracker
}

func (l *rpcExecutionLedger) stableAdmissionSafeFloor() uint64 {
	if l == nil {
		return 0
	}
	return l.activeAdmissions.stableSafeFloor(&l.nextAdmissionSeq)
}

type rpcExecutionLedgerShard struct {
	mu           sync.Mutex
	now          func() time.Time
	ttl          time.Duration
	receiptCount *atomic.Int64
	order        *list.List
	byKey        map[rpcExecutionKey]*list.Element
	// In-flight owners are independent from receipt TTL and cannot disappear
	// under completed-receipt pressure.
	pending map[rpcExecutionKey]*rpcResultFlight
}

type rpcExecutionLedgerCapacity struct {
	maxPending             int
	maxPendingPerAuth      int
	globalMaxEntries       int
	authMaxEntries         int
	sessionMaxEntries      int
	subscriberMaxGlobal    int
	subscriberMaxAuth      int
	subscriberMaxSession   int
	subscriberMaxPerFlight int
	replayStore            rpcReplayStore
}

func newRPCExecutionLedger(now func() time.Time, capacity rpcExecutionLedgerCapacity) *rpcExecutionLedger {
	if now == nil {
		now = time.Now
	}
	if capacity.replayStore == nil {
		panic("mtprotoedge: rpc execution ledger requires a replay store")
	}
	if capacity.maxPending <= 0 {
		capacity.maxPending = rpcResultFlightDefaultMaxPending
	}
	if capacity.maxPendingPerAuth <= 0 {
		capacity.maxPendingPerAuth = capacity.maxPending
	}
	if capacity.globalMaxEntries <= 0 {
		capacity.globalMaxEntries = rpcExecutionMaxEntries
	}
	if capacity.authMaxEntries <= 0 {
		capacity.authMaxEntries = capacity.globalMaxEntries
	}
	if capacity.sessionMaxEntries <= 0 {
		capacity.sessionMaxEntries = capacity.authMaxEntries
	}
	if capacity.subscriberMaxGlobal <= 0 {
		capacity.subscriberMaxGlobal = rpcResultSubscriberMaxGlobal
	}
	if capacity.subscriberMaxAuth <= 0 {
		capacity.subscriberMaxAuth = rpcResultSubscriberMaxAuth
	}
	if capacity.subscriberMaxSession <= 0 {
		capacity.subscriberMaxSession = rpcResultSubscriberMaxSession
	}
	if capacity.subscriberMaxPerFlight <= 0 {
		capacity.subscriberMaxPerFlight = rpcResultSubscriberMaxPerFlight
	}

	l := &rpcExecutionLedger{hashSeed: maphash.MakeSeed(), replayStore: capacity.replayStore}
	l.reservedEntries.max = int64(capacity.globalMaxEntries)
	l.flightLimit.max = int64(capacity.maxPending)
	l.fairBudget = newRPCExecutionFairBudget(
		l.hashSeed,
		&l.reservedEntries,
		int64(capacity.authMaxEntries),
		int64(capacity.sessionMaxEntries),
		capacity.maxPendingPerAuth,
	)
	l.subscriberBudget = newRPCResultSubscriberBudget(
		l.hashSeed,
		capacity.subscriberMaxGlobal,
		capacity.subscriberMaxAuth,
		capacity.subscriberMaxSession,
	)
	l.subscriberPerFlight = capacity.subscriberMaxPerFlight
	for i := range l.shards {
		s := &l.shards[i]
		s.now = now
		s.ttl = rpcExecutionReceiptTTL
		s.receiptCount = &l.receiptCount
		s.order = list.New()
		s.byKey = make(map[rpcExecutionKey]*list.Element)
		s.pending = make(map[rpcExecutionKey]*rpcResultFlight)
	}
	return l
}

func (l *rpcExecutionLedger) shard(key rpcExecutionKey) *rpcExecutionLedgerShard {
	return &l.shards[l.shardIndex(key)]
}

func (l *rpcExecutionLedger) shardIndex(key rpcExecutionKey) uint64 {
	var raw [24]byte
	copy(raw[:8], key.authKeyID[:])
	binary.LittleEndian.PutUint64(raw[8:16], uint64(key.sessionID))
	binary.LittleEndian.PutUint64(raw[16:24], uint64(key.reqMsgID))
	return maphash.Bytes(l.hashSeed, raw[:]) & (rpcExecutionLedgerShards - 1)
}

// Replay resolves an immutable result descriptor from the logical-session
// outbox. A receipt hit without an outbox frame is never treated as permission
// to execute the business handler again.
func (l *rpcExecutionLedger) Replay(authKeyID [8]byte, sessionID, reqMsgID int64) (*encodedOutboundMessage, bool) {
	if l == nil || reqMsgID == 0 {
		return nil, false
	}
	key := rpcExecutionKey{authKeyID: authKeyID, sessionID: sessionID, reqMsgID: reqMsgID}
	s := l.shard(key)
	now := s.now()
	s.mu.Lock()
	elem := s.byKey[key]
	if elem == nil {
		s.mu.Unlock()
		return nil, false
	}
	receipt := elem.Value.(*rpcExecutionReceipt)
	if !receipt.expiresAt.After(now) {
		s.removeElement(elem)
		s.mu.Unlock()
		return nil, false
	}
	if receipt.unavailable || receipt.acknowledged {
		s.mu.Unlock()
		return nil, false
	}
	s.mu.Unlock()
	return l.replayStore.rpcResult(authKeyID, sessionID, reqMsgID)
}

// Acknowledge removes a completed receipt immediately. reqMsgID must already
// have been resolved from the outbound actor's trusted server-msg-id mapping.
// A pending flight records the ACK so a racing completion cannot resurrect a
// receipt after the outbox body has been released.
func (l *rpcExecutionLedger) Acknowledge(authKeyID [8]byte, sessionID, reqMsgID int64) bool {
	if l == nil || reqMsgID == 0 {
		return false
	}
	key := rpcExecutionKey{authKeyID: authKeyID, sessionID: sessionID, reqMsgID: reqMsgID}
	s := l.shard(key)
	s.mu.Lock()
	s.expireLocked(s.now())
	if elem := s.byKey[key]; elem != nil {
		s.removeElement(elem)
		s.mu.Unlock()
		return true
	}
	if flight := s.pending[key]; flight != nil {
		flight.acknowledged = true
		s.mu.Unlock()
		return true
	}
	s.mu.Unlock()
	return false
}

// ObserveDependency returns a waiter for an admitted in-flight dependency, a
// completed execution outcome, or ok=false for an unknown request. It never
// creates execution ownership.
func (l *rpcExecutionLedger) ObserveDependency(authKeyID [8]byte, sessionID, reqMsgID int64) (rpcResultDependency, bool) {
	if l == nil || reqMsgID == 0 {
		return rpcResultDependency{}, false
	}
	key := rpcExecutionKey{authKeyID: authKeyID, sessionID: sessionID, reqMsgID: reqMsgID}
	s := l.shard(key)
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if elem := s.byKey[key]; elem != nil {
		receipt := elem.Value.(*rpcExecutionReceipt)
		if receipt.expiresAt.After(now) {
			if !receipt.executionKnown {
				return rpcResultDependency{}, false
			}
			return rpcResultDependency{completed: true, success: receipt.executionOK}, true
		}
		s.removeElement(elem)
	}
	if flight := s.pending[key]; flight != nil {
		if flight.executionDone {
			return rpcResultDependency{completed: true, success: flight.executionOK}, true
		}
		return rpcResultDependency{waiter: &rpcResultWaiter{ledger: l, key: key, flight: flight}}, true
	}
	return rpcResultDependency{}, false
}

// Complete publishes terminal execution metadata and resolves current
// waiters. replayable is only a claim from the egress path; the ledger verifies
// that replayStore actually owns the exact frame before publishing a replayable
// receipt. encoded is passed transiently to joined waiters and is never stored.
func (l *rpcExecutionLedger) Complete(
	authKeyID [8]byte,
	sessionID, reqMsgID int64,
	encoded *encodedOutboundMessage,
	replayable bool,
) {
	if l == nil || reqMsgID == 0 || encoded == nil {
		return
	}
	if replayable {
		_, replayable = l.replayStore.rpcResult(authKeyID, sessionID, reqMsgID)
	}
	if l.completeOnce(authKeyID, sessionID, reqMsgID, encoded, replayable) {
		return
	}
	// An expired receipt in another shard may be the only global blocker.
	l.expireReceipts()
	_ = l.completeOnce(authKeyID, sessionID, reqMsgID, encoded, replayable)
}

// completeOnce returns false only when a cross-shard expiry reap may release
// the global reservation required by a defensive completion without an owner.
func (l *rpcExecutionLedger) completeOnce(
	authKeyID [8]byte,
	sessionID, reqMsgID int64,
	encoded *encodedOutboundMessage,
	replayable bool,
) bool {
	key := rpcExecutionKey{authKeyID: authKeyID, sessionID: sessionID, reqMsgID: reqMsgID}
	s := l.shard(key)
	s.mu.Lock()
	now := s.now()
	s.expireLocked(now)
	old := s.byKey[key]
	flight := s.pending[key]
	var oldReceipt *rpcExecutionReceipt
	if old != nil {
		oldReceipt = old.Value.(*rpcExecutionReceipt)
		if flight == nil {
			// Terminal publication is immutable. Late callbacks cannot extend TTL,
			// replace replay identity or resurrect an acknowledged result.
			s.mu.Unlock()
			return true
		}
	}

	identity, admissionSeq, executionKnown, executionOK, acknowledged := rpcResultFlightMetadataLocked(s, key)
	var reservation *rpcExecutionBudgetReservation
	switch {
	case flight != nil:
		reservation = flight.reservation
		if reservation == nil {
			s.mu.Unlock()
			panic("mtprotoedge: pending rpc execution has no fair-budget reservation")
		}
	case oldReceipt != nil && oldReceipt.reservation != nil:
		reservation = oldReceipt.reservation
	default:
		reservation = l.fairBudget.reserveCompleted(key)
		if reservation == nil {
			s.mu.Unlock()
			return false
		}
	}

	if old != nil {
		s.unlinkElement(old)
		if oldReceipt.reservation != nil && oldReceipt.reservation != reservation {
			oldReceipt.reservation.release()
			oldReceipt.reservation = nil
		}
	}
	receipt := &rpcExecutionReceipt{
		key:            key,
		expiresAt:      now.Add(s.ttl),
		identity:       identity,
		admissionSeq:   admissionSeq,
		executionKnown: executionKnown,
		executionOK:    executionOK,
		acknowledged:   acknowledged,
		unavailable:    !replayable,
		reservation:    reservation,
	}
	elem := s.order.PushBack(receipt)
	s.byKey[key] = elem
	s.incrementReceiptCount()

	subscribers, executionSubscribers, terminalExecutionOK := l.completeRPCResultFlightLocked(s, key, encoded)
	if acknowledged {
		// ACK won before completion. Current subscribers still receive encoded,
		// but no post-ACK receipt survives this critical section.
		s.removeElement(elem)
	}
	s.mu.Unlock()
	for _, subscriber := range subscribers {
		subscriber(encoded, true)
	}
	for _, subscriber := range executionSubscribers {
		subscriber(terminalExecutionOK)
	}
	return true
}

func rpcResultFlightMetadataLocked(s *rpcExecutionLedgerShard, key rpcExecutionKey) (
	rpcResultRequestIdentity,
	uint64,
	bool,
	bool,
	bool,
) {
	if flight := s.pending[key]; flight != nil {
		return flight.identity, flight.admissionSeq, flight.executionDone, flight.executionOK, flight.acknowledged
	}
	return rpcResultRequestIdentity{}, 0, false, false, false
}

func (l *rpcExecutionLedger) expireReceipts() {
	if l == nil {
		return
	}
	for i := range l.shards {
		s := &l.shards[i]
		s.mu.Lock()
		s.expireLocked(s.now())
		s.mu.Unlock()
	}
}

func (l *rpcExecutionLedger) receiptBudgetBytes() int64 {
	if l == nil {
		return 0
	}
	return l.receiptCount.Load() * rpcExecutionReceiptBudgetBytes
}

func (l *rpcExecutionLedger) Close() error { return nil }

func (l *rpcExecutionLedger) CloseContext(context.Context) error { return nil }

// forgetSession removes every terminal receipt for a destroyed logical
// session. SessionManager calls it only after physical producers converge.
func (l *rpcExecutionLedger) forgetSession(authKeyID [8]byte, sessionID int64) {
	if l == nil {
		return
	}
	for i := range l.shards {
		s := &l.shards[i]
		s.mu.Lock()
		for elem := s.order.Front(); elem != nil; {
			next := elem.Next()
			receipt := elem.Value.(*rpcExecutionReceipt)
			if receipt.key.authKeyID == authKeyID && receipt.key.sessionID == sessionID {
				s.removeElement(elem)
			}
			elem = next
		}
		s.mu.Unlock()
	}
}

func (s *rpcExecutionLedgerShard) expireLocked(now time.Time) {
	for elem := s.order.Front(); elem != nil; {
		next := elem.Next()
		receipt := elem.Value.(*rpcExecutionReceipt)
		if receipt.expiresAt.After(now) {
			return
		}
		s.removeElement(elem)
		elem = next
	}
}

func (s *rpcExecutionLedgerShard) removeElement(elem *list.Element) {
	receipt := s.unlinkElement(elem)
	if receipt != nil && receipt.reservation != nil {
		receipt.reservation.release()
		receipt.reservation = nil
	}
}

func (s *rpcExecutionLedgerShard) unlinkElement(elem *list.Element) *rpcExecutionReceipt {
	if elem == nil {
		return nil
	}
	receipt := elem.Value.(*rpcExecutionReceipt)
	delete(s.byKey, receipt.key)
	s.order.Remove(elem)
	s.decrementReceiptCount()
	return receipt
}

func (s *rpcExecutionLedgerShard) incrementReceiptCount() {
	if s.receiptCount == nil {
		panic("mtprotoedge: rpc execution receipt counter is unavailable")
	}
	s.receiptCount.Add(1)
}

func (s *rpcExecutionLedgerShard) decrementReceiptCount() {
	if s.receiptCount == nil || s.receiptCount.Add(-1) < 0 {
		panic("mtprotoedge: rpc execution receipt counter underflow")
	}
}
