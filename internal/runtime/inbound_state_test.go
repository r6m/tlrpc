package runtime

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
)

func TestNewInboundStateLedgerValidatesBounds(t *testing.T) {
	t.Parallel()

	_, err := NewInboundStateLedger(InboundStateConfig{TTL: time.Second})
	if !errors.Is(err, ErrInvalidInboundStateCapacity) {
		t.Fatalf("zero capacity error = %v", err)
	}
	_, err = NewInboundStateLedger(InboundStateConfig{Capacity: 1})
	if !errors.Is(err, ErrInvalidInboundStateTTL) {
		t.Fatalf("zero TTL error = %v", err)
	}
}

func TestInboundStateLedgerRecordsAndCompletesCanonicalStates(t *testing.T) {
	t.Parallel()

	ledger := newTestInboundStateLedger(t, 4, nil)
	if err := ledger.Record(testInboundMessage(100, true)); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Record(testInboundMessage(300, false)); err != nil {
		t.Fatal(err)
	}
	ledger.Complete([]int64{100, 999}, true, true)

	want := []byte{
		mtprototl.MessageStateReceived |
			mtprototl.MessageStateKnown |
			mtprototl.MessageStateProcessing |
			mtprototl.MessageStateAcknowledged |
			mtprototl.MessageStateResponseGenerated,
		mtprototl.MessageStateNotReceived,
		mtprototl.MessageStateReceived |
			mtprototl.MessageStateKnown |
			mtprototl.MessageStateNoAcknowledgement,
		mtprototl.MessageStateUnknownTooOld,
		mtprototl.MessageStateUnknownTooHigh,
	}
	got := ledger.StateInfo([]int64{100, 200, 300, 50, 400})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("StateInfo() = %v, want %v", got, want)
	}
}

func TestInboundStateLedgerPreservesRequestedOrderingAndDetachedOutput(t *testing.T) {
	t.Parallel()

	ledger := newTestInboundStateLedger(t, 4, nil)
	for _, messageID := range []int64{10, 30} {
		if err := ledger.Record(testInboundMessage(messageID, true)); err != nil {
			t.Fatal(err)
		}
	}

	want := []byte{
		mtprototl.MessageStateUnknownTooHigh,
		mtprototl.MessageStateReceived | mtprototl.MessageStateKnown,
		mtprototl.MessageStateNotReceived,
		mtprototl.MessageStateReceived | mtprototl.MessageStateKnown,
		mtprototl.MessageStateUnknownTooOld,
	}
	got := ledger.StateInfo([]int64{40, 10, 20, 30, 5})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("StateInfo() = %v, want %v", got, want)
	}
	got[1] = 0
	if again := ledger.StateInfo([]int64{10}); !reflect.DeepEqual(again, want[1:2]) {
		t.Fatalf("mutating output changed ledger state: %v", again)
	}
}

func TestInboundStateLedgerExpiresAtTTL(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	ledger := newTestInboundStateLedger(t, 2, func() time.Time { return now })
	if err := ledger.Record(testInboundMessage(100, true)); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if got := ledger.Len(); got != 0 {
		t.Fatalf("Len() after TTL = %d, want 0", got)
	}
	if got := ledger.StateInfo([]int64{100}); !reflect.DeepEqual(got, []byte{mtprototl.MessageStateUnknownTooOld}) {
		t.Fatalf("expired StateInfo() = %v", got)
	}
}

func TestInboundStateLedgerEvictsDeterministicallyAtCapacity(t *testing.T) {
	t.Parallel()

	fixedNow := time.Unix(1_700_000_000, 0)
	ledger := newTestInboundStateLedger(t, 2, func() time.Time { return fixedNow })
	for _, messageID := range []int64{10, 20} {
		if err := ledger.Record(testInboundMessage(messageID, true)); err != nil {
			t.Fatal(err)
		}
	}
	if err := ledger.Record(testInboundMessage(10, true)); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Record(testInboundMessage(30, true)); err != nil {
		t.Fatal(err)
	}

	if got := ledger.Len(); got != 2 {
		t.Fatalf("Len() = %d, want strict capacity 2", got)
	}
	want := []byte{
		mtprototl.MessageStateNotReceived,
		mtprototl.MessageStateReceived | mtprototl.MessageStateKnown,
		mtprototl.MessageStateReceived | mtprototl.MessageStateKnown,
	}
	if got := ledger.StateInfo([]int64{20, 10, 30}); !reflect.DeepEqual(got, want) {
		t.Fatalf("StateInfo() after eviction = %v, want %v", got, want)
	}
}

func TestInboundStateLedgerRejectsInvalidMessage(t *testing.T) {
	t.Parallel()

	ledger := newTestInboundStateLedger(t, 1, nil)
	if err := ledger.Record(InboundMessage{MessageID: 1}); !errors.Is(err, ErrInvalidInbound) {
		t.Fatalf("Record() error = %v, want %v", err, ErrInvalidInbound)
	}
	if got := ledger.Len(); got != 0 {
		t.Fatalf("Len() = %d after rejected record", got)
	}
}

func TestInboundStateLedgerConcurrentAccess(t *testing.T) {
	t.Parallel()

	ledger := newTestInboundStateLedger(t, 32, nil)
	var workers sync.WaitGroup
	for worker := int64(1); worker <= 8; worker++ {
		workers.Add(1)
		go func(messageID int64) {
			defer workers.Done()
			for range 100 {
				if err := ledger.Record(testInboundMessage(messageID, true)); err != nil {
					t.Errorf("Record(%d): %v", messageID, err)
					return
				}
				ledger.Complete([]int64{messageID}, true, true)
				_ = ledger.StateInfo([]int64{messageID})
			}
		}(worker)
	}
	workers.Wait()
	if got := ledger.Len(); got != 8 {
		t.Fatalf("Len() = %d, want 8", got)
	}
}

func newTestInboundStateLedger(t *testing.T, capacity int, now func() time.Time) *InboundStateLedger {
	t.Helper()
	ledger, err := NewInboundStateLedger(InboundStateConfig{
		Capacity: capacity,
		TTL:      time.Minute,
		Now:      now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}

func testInboundMessage(messageID int64, contentRelated bool) InboundMessage {
	const constructorID = uint32(0x11223344)
	return InboundMessage{
		MessageID:      messageID,
		SequenceNo:     1,
		ConstructorID:  constructorID,
		Body:           []byte{0x44, 0x33, 0x22, 0x11},
		ContentRelated: contentRelated,
	}
}
