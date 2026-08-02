package mtprotoedge

import (
	"container/list"
	"errors"
	"sync"
	"testing"
	"time"
	"unsafe"
)

func newRPCExecutionLedgerWithLimitsForTest(
	now func() time.Time,
	maxPending, maxPendingPerAuth, global, auth, session int,
) *rpcExecutionLedger {
	return newRPCExecutionLedger(now, rpcExecutionLedgerCapacity{
		maxPending: maxPending, maxPendingPerAuth: maxPendingPerAuth,
		globalMaxEntries: global, authMaxEntries: auth, sessionMaxEntries: session,
		replayStore: newRPCReplayStoreForTest(),
	})
}

func TestRPCExecutionLedgerSessionCapacityIsolatesAnotherAuth(t *testing.T) {
	ledger := newRPCExecutionLedgerWithLimitsForTest(time.Now, 8, 6, 8, 6, 2)
	authA := [8]byte{0xa1}
	authB := [8]byte{0xb1}
	for i := 0; i < 2; i++ {
		msgID := int64(1000 + i)
		claim, err := ledger.Acquire(authA, 77, msgID)
		if err != nil || claim.state != rpcResultAcquireOwner {
			t.Fatalf("same-session admission %d = %#v, %v", i, claim, err)
		}
		ledger.completeReplayableForTest(authA, 77, msgID, &encodedOutboundMessage{body: []byte{1}})
	}
	if _, err := ledger.Acquire(authA, 77, 2000); !errors.Is(err, ErrRPCResultFlightCapacity) {
		t.Fatalf("admission beyond session limit = %v, want capacity", err)
	}
	other, err := ledger.Acquire(authB, 88, 3000)
	if err != nil || other.state != rpcResultAcquireOwner {
		t.Fatalf("other auth blocked by full session: %#v, %v", other, err)
	}
	other.owner.Abort()
}

func TestRPCExecutionLedgerAuthCapacityIsolatesAnotherAuth(t *testing.T) {
	ledger := newRPCExecutionLedgerWithLimitsForTest(time.Now, 8, 4, 8, 2, 2)
	authA := [8]byte{0xa2}
	authB := [8]byte{0xb2}
	for i := 0; i < 2; i++ {
		claim, err := ledger.Acquire(authA, int64(10+i), int64(100+i))
		if err != nil || claim.state != rpcResultAcquireOwner {
			t.Fatalf("auth A admission %d = %#v, %v", i, claim, err)
		}
		ledger.completeReplayableForTest(authA, int64(10+i), int64(100+i), &encodedOutboundMessage{body: []byte{1}})
	}
	if _, err := ledger.Acquire(authA, 12, 102); !errors.Is(err, ErrRPCResultFlightCapacity) {
		t.Fatalf("same auth beyond limit = %v, want capacity", err)
	}
	other, err := ledger.Acquire(authB, 20, 200)
	if err != nil || other.state != rpcResultAcquireOwner {
		t.Fatalf("other auth blocked by auth A: %#v, %v", other, err)
	}
	other.owner.Abort()
}

func TestRPCExecutionLedgerPendingLimitIsAdditional(t *testing.T) {
	ledger := newRPCExecutionLedgerWithLimitsForTest(time.Now, 6, 2, 12, 6, 4)
	authA := [8]byte{0xa3}
	authB := [8]byte{0xb3}
	owners := make([]*rpcResultOwnerLease, 0, 3)
	for i := 0; i < 2; i++ {
		claim, err := ledger.Acquire(authA, int64(i+1), int64(100+i))
		if err != nil || claim.state != rpcResultAcquireOwner {
			t.Fatalf("pending auth A %d = %#v, %v", i, claim, err)
		}
		owners = append(owners, claim.owner)
	}
	if _, err := ledger.Acquire(authA, 3, 103); !errors.Is(err, ErrRPCResultFlightCapacity) {
		t.Fatalf("third pending owner for auth A = %v, want capacity", err)
	}
	other, err := ledger.Acquire(authB, 4, 104)
	if err != nil || other.state != rpcResultAcquireOwner {
		t.Fatalf("auth B blocked by auth A pending limit: %#v, %v", other, err)
	}
	owners = append(owners, other.owner)
	for _, owner := range owners {
		if !owner.Abort() {
			t.Fatal("pending owner did not abort")
		}
	}
	if usage := ledger.fairBudget.authSnapshot(authA); usage != (rpcExecutionBudgetUsage{}) {
		t.Fatalf("auth A budget after abort = %#v", usage)
	}
}

func TestRPCExecutionLedgerReceiptLifecycleACKAndTTL(t *testing.T) {
	now := time.Unix(1000, 0)
	ledger := newRPCExecutionLedgerWithLimitsForTest(func() time.Time { return now }, 4, 4, 6, 5, 3)
	auth := [8]byte{0xc1}

	claim, err := ledger.Acquire(auth, 1, 101)
	if err != nil || claim.state != rpcResultAcquireOwner {
		t.Fatalf("owner = %#v, %v", claim, err)
	}
	if usage := ledger.fairBudget.authSnapshot(auth); usage.entries != 1 || usage.pending != 1 {
		t.Fatalf("pending reservation = %#v", usage)
	}
	claim.owner.CompleteExecution(true)
	ledger.completeReplayableForTest(auth, 1, 101, &encodedOutboundMessage{body: make([]byte, 8<<20)})
	if ledger.flightLimit.snapshot() != 0 || ledger.receiptCount.Load() != 1 || ledger.reservedEntries.snapshot() != 1 {
		t.Fatalf("terminal counts owner=%d receipt=%d reserved=%d", ledger.flightLimit.snapshot(), ledger.receiptCount.Load(), ledger.reservedEntries.snapshot())
	}
	if got := ledger.receiptBudgetBytes(); got != rpcExecutionReceiptBudgetBytes {
		t.Fatalf("8 MiB result charged %d receipt bytes, want fixed %d", got, rpcExecutionReceiptBudgetBytes)
	}
	if !ledger.Acknowledge(auth, 1, 101) {
		t.Fatal("ACK did not remove receipt")
	}
	if ledger.receiptCount.Load() != 0 || ledger.reservedEntries.snapshot() != 0 || ledger.receiptBudgetBytes() != 0 {
		t.Fatal("ACK leaked receipt reservation")
	}

	second, err := ledger.Acquire(auth, 2, 201)
	if err != nil || second.state != rpcResultAcquireOwner {
		t.Fatalf("second owner = %#v, %v", second, err)
	}
	ledger.completeReplayableForTest(auth, 2, 201, &encodedOutboundMessage{body: []byte{1}})
	now = now.Add(rpcExecutionReceiptTTL + time.Second)
	if _, ok := ledger.Replay(auth, 2, 201); ok {
		t.Fatal("expired receipt remained replayable")
	}
	if ledger.receiptCount.Load() != 0 || ledger.reservedEntries.snapshot() != 0 {
		t.Fatal("TTL leaked receipt reservation")
	}
}

func TestRPCExecutionLedgerACKBeforeCompleteDoesNotResurrectReceipt(t *testing.T) {
	ledger := newRPCExecutionLedgerForTest(time.Now, 2)
	auth := [8]byte{0xd1}
	claim, err := ledger.Acquire(auth, 1, 101)
	if err != nil || claim.state != rpcResultAcquireOwner {
		t.Fatalf("owner = %#v, %v", claim, err)
	}
	joined, err := ledger.Acquire(auth, 1, 101)
	if err != nil || joined.state != rpcResultAcquirePending {
		t.Fatalf("join = %#v, %v", joined, err)
	}
	if !ledger.Acknowledge(auth, 1, 101) {
		t.Fatal("ACK did not mark pending owner")
	}
	want := &encodedOutboundMessage{body: []byte{1}, reqMsgID: 101}
	ledger.completeReplayableForTest(auth, 1, 101, want)
	if got, ok, waitErr := joined.waiter.Wait(t.Context()); waitErr != nil || !ok || got != want {
		t.Fatalf("joined waiter = %p/%v/%v", got, ok, waitErr)
	}
	if ledger.receiptCount.Load() != 0 || ledger.reservedEntries.snapshot() != 0 {
		t.Fatal("ACK-before-complete resurrected receipt")
	}
	newClaim, err := ledger.Acquire(auth, 1, 101)
	if err != nil || newClaim.state != rpcResultAcquireOwner {
		t.Fatalf("post-ACK request did not get a fresh owner: %#v, %v", newClaim, err)
	}
	newClaim.owner.Abort()
}

func TestRPCExecutionLedgerUnavailableTombstonePreventsReexecution(t *testing.T) {
	now := time.Unix(1000, 0)
	ledger := newRPCExecutionLedgerWithLimitsForTest(func() time.Time { return now }, 2, 2, 4, 4, 4)
	auth := [8]byte{0xe1}
	claim, err := ledger.Acquire(auth, 1, 101)
	if err != nil || claim.state != rpcResultAcquireOwner {
		t.Fatalf("owner = %#v, %v", claim, err)
	}
	claim.owner.CompleteExecution(true)
	ledger.Complete(auth, 1, 101, &encodedOutboundMessage{body: make([]byte, 8<<20)}, false)
	if _, ok := ledger.Replay(auth, 1, 101); ok {
		t.Fatal("unavailable tombstone masqueraded as replayable")
	}
	if _, err := ledger.Acquire(auth, 1, 101); !errors.Is(err, ErrRPCResultFlightCapacity) {
		t.Fatalf("duplicate after unavailable completion = %v, want capacity", err)
	}
	if got := ledger.receiptBudgetBytes(); got != rpcExecutionReceiptBudgetBytes {
		t.Fatalf("unavailable receipt budget = %d", got)
	}
	now = now.Add(rpcExecutionReceiptTTL + time.Second)
	retry, err := ledger.Acquire(auth, 1, 101)
	if err != nil || retry.state != rpcResultAcquireOwner {
		t.Fatalf("admission after tombstone expiry = %#v, %v", retry, err)
	}
	retry.owner.Abort()
}

func TestRPCExecutionLedgerConcurrentReservationsNeverOvercommit(t *testing.T) {
	const limit = 24
	ledger := newRPCExecutionLedgerWithLimitsForTest(time.Now, limit, 4, limit, 8, 3)
	const callers = 256
	start := make(chan struct{})
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		owners []*rpcResultOwnerLease
	)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			auth := [8]byte{byte(i % 4)}
			claim, err := ledger.Acquire(auth, int64(i%8), int64(1000+i))
			if errors.Is(err, ErrRPCResultFlightCapacity) {
				return
			}
			if err != nil || claim.state != rpcResultAcquireOwner {
				t.Errorf("Acquire %d = %#v, %v", i, claim, err)
				return
			}
			mu.Lock()
			owners = append(owners, claim.owner)
			mu.Unlock()
		}(i)
	}
	close(start)
	wg.Wait()
	if got := ledger.reservedEntries.snapshot(); got > limit || got != int64(len(owners)) {
		t.Fatalf("reserved=%d owners=%d limit=%d", got, len(owners), limit)
	}
	for i := 0; i < 4; i++ {
		auth := [8]byte{byte(i)}
		usage := ledger.fairBudget.authSnapshot(auth)
		if usage.entries > 8 || usage.pending > 4 {
			t.Fatalf("auth %d overcommitted: %#v", i, usage)
		}
	}
	for _, owner := range owners {
		owner.Abort()
	}
	if ledger.reservedEntries.snapshot() != 0 {
		t.Fatal("concurrent abort leaked reservations")
	}
}

func TestRPCExecutionLedgerFullKeyHashSpreadsOneSession(t *testing.T) {
	first := newRPCExecutionLedgerForTest(time.Now, 64)
	second := newRPCExecutionLedgerForTest(time.Now, 64)
	auth := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	seen := make(map[uint64]struct{})
	differentInstance := false
	for msgID := int64(1); msgID <= 256; msgID++ {
		key := rpcExecutionKey{authKeyID: auth, sessionID: 99, reqMsgID: msgID}
		firstIndex := first.shardIndex(key)
		seen[firstIndex] = struct{}{}
		if firstIndex != second.shardIndex(key) {
			differentInstance = true
		}
	}
	if len(seen) < rpcExecutionLedgerShards/2 {
		t.Fatalf("one session used only %d/%d shards", len(seen), rpcExecutionLedgerShards)
	}
	if !differentInstance {
		t.Fatal("two ledger instances used an identical shard stream")
	}
}

func TestRPCExecutionLedgerForgetSessionReleasesReceipts(t *testing.T) {
	ledger := newRPCExecutionLedgerForTest(time.Now, 8)
	auth := [8]byte{0xf1}
	for _, sessionID := range []int64{1, 1, 2} {
		msgID := int64(100 + ledger.receiptCount.Load())
		claim, err := ledger.Acquire(auth, sessionID, msgID)
		if err != nil || claim.state != rpcResultAcquireOwner {
			t.Fatalf("owner session=%d: %#v, %v", sessionID, claim, err)
		}
		ledger.completeReplayableForTest(auth, sessionID, msgID, &encodedOutboundMessage{body: []byte{1}})
	}
	ledger.forgetSession(auth, 1)
	if got := ledger.receiptCount.Load(); got != 1 {
		t.Fatalf("receipts after session forget = %d, want 1", got)
	}
	if _, ok := ledger.Replay(auth, 1, 100); ok {
		t.Fatal("forgotten session remained replayable")
	}
}

func TestRPCExecutionLedgerServerOptionsPropagateLimits(t *testing.T) {
	s := New(Options{
		RPCGlobalMaxTasks:             6,
		RPCExecutionMaxEntries:        12,
		RPCExecutionAuthMaxEntries:    8,
		RPCExecutionSessionMaxEntries: 4,
		RPCExecutionPendingPerAuth:    3,
	})
	if s.rpcResults.reservedEntries.max != 12 {
		t.Fatalf("global option propagation = %d", s.rpcResults.reservedEntries.max)
	}
	budget := s.rpcResults.fairBudget
	if budget.authLimit != 8 || budget.sessionLimit != 4 || budget.pendingPerAuth != 3 {
		t.Fatalf("fair option propagation = auth:%d session:%d pending:%d", budget.authLimit, budget.sessionLimit, budget.pendingPerAuth)
	}
}

func TestRPCExecutionLedgerServerOptionsFailFast(t *testing.T) {
	base := Options{
		RPCGlobalMaxTasks:             6,
		RPCExecutionMaxEntries:        12,
		RPCExecutionAuthMaxEntries:    8,
		RPCExecutionSessionMaxEntries: 4,
		RPCExecutionPendingPerAuth:    3,
	}
	tests := []struct {
		name   string
		mutate func(*Options)
	}{
		{name: "entry hierarchy", mutate: func(o *Options) { o.RPCExecutionAuthMaxEntries = 13 }},
		{name: "pending hierarchy", mutate: func(o *Options) { o.RPCExecutionPendingPerAuth = 7 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := base
			test.mutate(&opts)
			defer func() {
				if recover() == nil {
					t.Fatal("New accepted invalid rpc execution options")
				}
			}()
			_ = New(opts)
		})
	}
}

func TestRPCExecutionLedgerRequiresReplayStore(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ledger accepted nil replay store")
		}
	}()
	_ = newRPCExecutionLedger(time.Now, rpcExecutionLedgerCapacity{})
}

func TestRPCExecutionReceiptBudgetCoversOwnedFixedStructures(t *testing.T) {
	fixed := unsafe.Sizeof(rpcExecutionReceipt{}) +
		unsafe.Sizeof(list.Element{}) +
		unsafe.Sizeof(rpcExecutionBudgetReservation{})
	if fixed > rpcExecutionReceiptBudgetBytes {
		t.Fatalf("fixed receipt structures use %d bytes, budget charge is %d", fixed, rpcExecutionReceiptBudgetBytes)
	}
}
