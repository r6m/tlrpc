package runtime

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/r6m/tlrpc/mtproto/reliability"
	"github.com/r6m/tlrpc/session"
)

func TestNewReliabilityRegistryValidatesBounds(t *testing.T) {
	t.Parallel()

	_, err := NewReliabilityRegistry(ReliabilityRegistryConfig{MessageCapacity: 1, TTL: time.Minute})
	if !errors.Is(err, ErrInvalidReliabilitySessionCapacity) {
		t.Fatalf("zero session capacity error = %v", err)
	}
	_, err = NewReliabilityRegistry(ReliabilityRegistryConfig{MaxSessions: 1, TTL: time.Minute})
	if !errors.Is(err, ErrInvalidReliabilityMessageCapacity) {
		t.Fatalf("zero message capacity error = %v", err)
	}
	_, err = NewReliabilityRegistry(ReliabilityRegistryConfig{MaxSessions: 1, MessageCapacity: 1})
	if !errors.Is(err, ErrInvalidReliabilityTTL) {
		t.Fatalf("zero TTL error = %v", err)
	}
}

func TestReliabilityRegistryReusesStateAcrossReconnect(t *testing.T) {
	t.Parallel()

	registry := newTestReliabilityRegistry(t, 2, nil)
	key := testReliabilitySessionKey(1)
	first, err := registry.Acquire(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.outboundStore().Put(reliability.SentMessage{MessageID: 101, Payload: []byte("retained")}); err != nil {
		t.Fatal(err)
	}
	if err := first.inboundLedger().Record(testInboundMessage(201, true)); err != nil {
		t.Fatal(err)
	}
	firstOutbound := first.outboundStore()
	firstInbound := first.inboundLedger()
	first.Release()

	reconnected, err := registry.Acquire(key)
	if err != nil {
		t.Fatal(err)
	}
	defer reconnected.Release()
	if reconnected.outboundStore() != firstOutbound || reconnected.inboundLedger() != firstInbound {
		t.Fatal("reconnect did not reuse exact reliability stores")
	}
	if message, ok := reconnected.outboundStore().Lookup(101); !ok || string(message.Payload) != "retained" {
		t.Fatalf("outbound state was not retained: %+v, %v", message, ok)
	}
	if got := reconnected.inboundLedger().Len(); got != 1 {
		t.Fatalf("inbound state length = %d, want 1", got)
	}
}

func TestReliabilityRegistryRejectsNewSessionWhenAllEntriesActive(t *testing.T) {
	t.Parallel()

	registry := newTestReliabilityRegistry(t, 1, nil)
	active, err := registry.Acquire(testReliabilitySessionKey(1))
	if err != nil {
		t.Fatal(err)
	}
	defer active.Release()

	_, err = registry.Acquire(testReliabilitySessionKey(2))
	var capacityError *ReliabilityCapacityError
	if !errors.As(err, &capacityError) {
		t.Fatalf("capacity error = %v, want *ReliabilityCapacityError", err)
	}
	if capacityError.MaxSessions != 1 {
		t.Fatalf("capacity max = %d, want 1", capacityError.MaxSessions)
	}
}

func TestReliabilityRegistryExpiresIdleState(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	registry := newTestReliabilityRegistry(t, 1, func() time.Time { return now })
	key := testReliabilitySessionKey(1)
	first, err := registry.Acquire(key)
	if err != nil {
		t.Fatal(err)
	}
	firstOutbound := first.outboundStore()
	firstInbound := first.inboundLedger()
	first.Release()

	now = now.Add(time.Minute)
	replacement, err := registry.Acquire(key)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Release()
	if replacement.outboundStore() == firstOutbound || replacement.inboundLedger() == firstInbound {
		t.Fatal("expired idle entry was reused")
	}
}

func TestReliabilityRegistryEvictsOldestIdleEntryDeterministically(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	registry := newTestReliabilityRegistry(t, 2, func() time.Time { return now })
	firstKey := testReliabilitySessionKey(1)
	secondKey := testReliabilitySessionKey(2)
	first, err := registry.Acquire(firstKey)
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.Acquire(secondKey)
	if err != nil {
		t.Fatal(err)
	}
	secondOutbound := second.outboundStore()
	first.Release()
	now = now.Add(time.Second)
	second.Release()

	third, err := registry.Acquire(testReliabilitySessionKey(3))
	if err != nil {
		t.Fatal(err)
	}
	defer third.Release()
	reconnectedSecond, err := registry.Acquire(secondKey)
	if err != nil {
		t.Fatal(err)
	}
	defer reconnectedSecond.Release()
	if reconnectedSecond.outboundStore() != secondOutbound {
		t.Fatal("newer idle entry was evicted instead of oldest idle entry")
	}
	if _, err := registry.Acquire(firstKey); err == nil {
		t.Fatal("evicted oldest entry was unexpectedly admitted while replacements are active")
	}
}

func TestReliabilityHandleReleaseIsIdempotentAndEntryScoped(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	registry := newTestReliabilityRegistry(t, 1, func() time.Time { return now })
	key := testReliabilitySessionKey(1)
	stale, err := registry.Acquire(key)
	if err != nil {
		t.Fatal(err)
	}
	stale.Release()
	now = now.Add(time.Minute)
	replacement, err := registry.Acquire(key)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Release()

	stale.Release()
	_, err = registry.Acquire(testReliabilitySessionKey(2))
	var capacityError *ReliabilityCapacityError
	if !errors.As(err, &capacityError) {
		t.Fatalf("stale release affected active replacement: %v", err)
	}
}

func TestReliabilityRegistryConcurrentSameSessionAcquire(t *testing.T) {
	t.Parallel()

	registry := newTestReliabilityRegistry(t, 1, nil)
	key := testReliabilitySessionKey(1)
	const workers = 32
	start := make(chan struct{})
	handles := make(chan *ReliabilityHandle, workers)
	errorsSeen := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			handle, err := registry.Acquire(key)
			if err != nil {
				errorsSeen <- err
				return
			}
			handles <- handle
		}()
	}
	close(start)
	group.Wait()
	close(handles)
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatalf("concurrent Acquire: %v", err)
	}

	var outbound *reliability.Store
	var inbound *InboundStateLedger
	acquired := make([]*ReliabilityHandle, 0, workers)
	for handle := range handles {
		if outbound == nil {
			outbound = handle.outboundStore()
			inbound = handle.inboundLedger()
		} else if handle.outboundStore() != outbound || handle.inboundLedger() != inbound {
			t.Fatal("concurrent acquisitions did not share exact stores")
		}
		acquired = append(acquired, handle)
	}
	if len(acquired) != workers {
		t.Fatalf("acquired %d handles, want %d", len(acquired), workers)
	}

	group = sync.WaitGroup{}
	for _, handle := range acquired {
		group.Add(1)
		go func(handle *ReliabilityHandle) {
			defer group.Done()
			handle.Release()
			handle.Release()
		}(handle)
	}
	group.Wait()
	replacement, err := registry.Acquire(testReliabilitySessionKey(2))
	if err != nil {
		t.Fatalf("idle entry was not evictable after concurrent releases: %v", err)
	}
	replacement.Release()
}

func newTestReliabilityRegistry(t *testing.T, maxSessions int, now func() time.Time) *ReliabilityRegistry {
	t.Helper()
	registry, err := NewReliabilityRegistry(ReliabilityRegistryConfig{
		MaxSessions:     maxSessions,
		MessageCapacity: 8,
		TTL:             time.Minute,
		Now:             now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func testReliabilitySessionKey(id int64) session.SessionKey {
	return session.SessionKey{AuthKeyID: 7, SessionID: id}
}
