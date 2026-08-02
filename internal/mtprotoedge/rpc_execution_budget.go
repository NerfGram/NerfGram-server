package mtprotoedge

import (
	"encoding/binary"
	"hash/maphash"
	"sync"
)

const rpcExecutionBudgetShards = 64

type rpcExecutionBudgetUsage struct {
	entries int64
	pending int64
}

type rpcExecutionSessionBudgetKey struct {
	authKeyID [8]byte
	sessionID int64
}

type rpcExecutionAuthBudgetShard struct {
	mu    sync.Mutex
	usage map[[8]byte]rpcExecutionBudgetUsage
}

type rpcExecutionSessionBudgetShard struct {
	mu    sync.Mutex
	usage map[rpcExecutionSessionBudgetKey]rpcExecutionBudgetUsage
}

// rpcExecutionFairBudget accounts one bounded ledger slot at global, auth and
// session scopes. Pending owner and completed receipt are the same ownership:
// Complete transfers the reservation; Abort/ACK/TTL return it.
type rpcExecutionFairBudget struct {
	seed           maphash.Seed
	globalEntries  *rpcResultFlightLimit
	authLimit      int64
	sessionLimit   int64
	pendingPerAuth int64
	authShards     [rpcExecutionBudgetShards]rpcExecutionAuthBudgetShard
	sessionShards  [rpcExecutionBudgetShards]rpcExecutionSessionBudgetShard
}

type rpcExecutionBudgetReservation struct {
	budget   *rpcExecutionFairBudget
	key      rpcExecutionKey
	pending  bool
	released bool
}

func newRPCExecutionFairBudget(
	seed maphash.Seed,
	globalEntries *rpcResultFlightLimit,
	authLimit int64,
	sessionLimit int64,
	pendingPerAuth int,
) *rpcExecutionFairBudget {
	b := &rpcExecutionFairBudget{
		seed: seed, globalEntries: globalEntries, authLimit: authLimit,
		sessionLimit: sessionLimit, pendingPerAuth: int64(pendingPerAuth),
	}
	for i := range b.authShards {
		b.authShards[i].usage = make(map[[8]byte]rpcExecutionBudgetUsage)
		b.sessionShards[i].usage = make(map[rpcExecutionSessionBudgetKey]rpcExecutionBudgetUsage)
	}
	return b
}

func (b *rpcExecutionFairBudget) reserveOwner(key rpcExecutionKey) *rpcExecutionBudgetReservation {
	return b.reserve(key, true)
}

func (b *rpcExecutionFairBudget) reserveCompleted(key rpcExecutionKey) *rpcExecutionBudgetReservation {
	return b.reserve(key, false)
}

func (b *rpcExecutionFairBudget) reserve(key rpcExecutionKey, pending bool) *rpcExecutionBudgetReservation {
	if b == nil || b.globalEntries == nil {
		return nil
	}
	authShard := b.authShard(key.authKeyID)
	sessionKey := rpcExecutionSessionBudgetKey{authKeyID: key.authKeyID, sessionID: key.sessionID}
	sessionShard := b.sessionShard(sessionKey)
	authShard.mu.Lock()
	sessionShard.mu.Lock()
	authUsage := authShard.usage[key.authKeyID]
	sessionUsage := sessionShard.usage[sessionKey]
	allowed := withinRPCExecutionBudget(authUsage.entries, 1, b.authLimit) &&
		withinRPCExecutionBudget(sessionUsage.entries, 1, b.sessionLimit)
	if pending {
		allowed = allowed && withinRPCExecutionBudget(authUsage.pending, 1, b.pendingPerAuth)
	}
	if !allowed || !b.globalEntries.reserve() {
		sessionShard.mu.Unlock()
		authShard.mu.Unlock()
		return nil
	}
	authUsage.entries++
	sessionUsage.entries++
	if pending {
		authUsage.pending++
	}
	authShard.usage[key.authKeyID] = authUsage
	sessionShard.usage[sessionKey] = sessionUsage
	sessionShard.mu.Unlock()
	authShard.mu.Unlock()
	return &rpcExecutionBudgetReservation{budget: b, key: key, pending: pending}
}

func withinRPCExecutionBudget(used, delta, limit int64) bool {
	return delta >= 0 && limit > 0 && used >= 0 && used <= limit-delta
}

func (r *rpcExecutionBudgetReservation) releasePending() {
	if r == nil || r.budget == nil || r.released || !r.pending {
		return
	}
	b := r.budget
	shard := b.authShard(r.key.authKeyID)
	shard.mu.Lock()
	usage, ok := shard.usage[r.key.authKeyID]
	if !ok || usage.pending < 1 {
		shard.mu.Unlock()
		panic("mtprotoedge: rpc execution per-auth pending budget underflow")
	}
	usage.pending--
	shard.usage[r.key.authKeyID] = usage
	r.pending = false
	shard.mu.Unlock()
}

func (r *rpcExecutionBudgetReservation) release() {
	if r == nil || r.budget == nil || r.released {
		return
	}
	b := r.budget
	authShard := b.authShard(r.key.authKeyID)
	sessionKey := rpcExecutionSessionBudgetKey{authKeyID: r.key.authKeyID, sessionID: r.key.sessionID}
	sessionShard := b.sessionShard(sessionKey)
	authShard.mu.Lock()
	sessionShard.mu.Lock()
	authUsage, authOK := authShard.usage[r.key.authKeyID]
	sessionUsage, sessionOK := sessionShard.usage[sessionKey]
	if !authOK || !sessionOK || authUsage.entries < 1 || sessionUsage.entries < 1 ||
		(r.pending && authUsage.pending < 1) {
		sessionShard.mu.Unlock()
		authShard.mu.Unlock()
		panic("mtprotoedge: rpc execution fair budget underflow")
	}
	authUsage.entries--
	sessionUsage.entries--
	if r.pending {
		authUsage.pending--
	}
	if authUsage == (rpcExecutionBudgetUsage{}) {
		delete(authShard.usage, r.key.authKeyID)
	} else {
		authShard.usage[r.key.authKeyID] = authUsage
	}
	if sessionUsage == (rpcExecutionBudgetUsage{}) {
		delete(sessionShard.usage, sessionKey)
	} else {
		sessionShard.usage[sessionKey] = sessionUsage
	}
	r.released = true
	r.pending = false
	b.globalEntries.release()
	sessionShard.mu.Unlock()
	authShard.mu.Unlock()
}

func (b *rpcExecutionFairBudget) authSnapshot(authKeyID [8]byte) rpcExecutionBudgetUsage {
	if b == nil {
		return rpcExecutionBudgetUsage{}
	}
	shard := b.authShard(authKeyID)
	shard.mu.Lock()
	usage := shard.usage[authKeyID]
	shard.mu.Unlock()
	return usage
}

func (b *rpcExecutionFairBudget) sessionSnapshot(authKeyID [8]byte, sessionID int64) rpcExecutionBudgetUsage {
	if b == nil {
		return rpcExecutionBudgetUsage{}
	}
	key := rpcExecutionSessionBudgetKey{authKeyID: authKeyID, sessionID: sessionID}
	shard := b.sessionShard(key)
	shard.mu.Lock()
	usage := shard.usage[key]
	shard.mu.Unlock()
	return usage
}

func (b *rpcExecutionFairBudget) authShard(authKeyID [8]byte) *rpcExecutionAuthBudgetShard {
	index := maphash.Bytes(b.seed, authKeyID[:]) & (rpcExecutionBudgetShards - 1)
	return &b.authShards[index]
}

func (b *rpcExecutionFairBudget) sessionShard(key rpcExecutionSessionBudgetKey) *rpcExecutionSessionBudgetShard {
	var raw [16]byte
	copy(raw[:8], key.authKeyID[:])
	binary.LittleEndian.PutUint64(raw[8:], uint64(key.sessionID))
	index := maphash.Bytes(b.seed, raw[:]) & (rpcExecutionBudgetShards - 1)
	return &b.sessionShards[index]
}
