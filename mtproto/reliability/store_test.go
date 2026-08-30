package reliability

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

func newTestStore(t *testing.T, capacity int, ttl time.Duration, clock *fakeClock) *Store {
	t.Helper()
	store, err := New(Config{Capacity: capacity, TTL: ttl, Now: clock.Now})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return store
}

func TestNewValidatesBounds(t *testing.T) {
	t.Parallel()

	if _, err := New(Config{TTL: time.Second}); !errors.Is(err, ErrInvalidCapacity) {
		t.Fatalf("New() error = %v, want %v", err, ErrInvalidCapacity)
	}
	if _, err := New(Config{Capacity: 1}); !errors.Is(err, ErrInvalidTTL) {
		t.Fatalf("New() error = %v, want %v", err, ErrInvalidTTL)
	}
}

func TestPutLookupAndPayloadOwnership(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	store := newTestStore(t, 2, time.Minute, clock)
	payload := []byte{1, 2, 3}
	if evicted, err := store.Put(SentMessage{MessageID: 4, SequenceNumber: 7, Payload: payload}); err != nil || evicted != nil {
		t.Fatalf("Put() = (%v, %v), want (nil, nil)", evicted, err)
	}
	payload[0] = 9

	got, ok := store.Lookup(4)
	if !ok {
		t.Fatal("Lookup() did not find message")
	}
	if got.SequenceNumber != 7 || got.SentAt != clock.Now() || got.Acknowledged {
		t.Fatalf("Lookup() = %+v", got)
	}
	if got.Payload[0] != 1 {
		t.Fatalf("stored payload changed through caller slice: %v", got.Payload)
	}
	got.Payload[1] = 9
	gotAgain, _ := store.Lookup(4)
	if gotAgain.Payload[1] != 2 {
		t.Fatalf("stored payload changed through returned slice: %v", gotAgain.Payload)
	}
}

func TestAcknowledgeAndLookupForResend(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	store := newTestStore(t, 2, time.Minute, clock)
	if _, err := store.Put(SentMessage{MessageID: 4, Payload: []byte("body")}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.LookupForResend(4); !ok {
		t.Fatal("unacknowledged message is not eligible for resend")
	}
	if !store.Acknowledge(4) {
		t.Fatal("Acknowledge() = false")
	}
	if _, ok := store.LookupForResend(4); ok {
		t.Fatal("acknowledged message is eligible for resend")
	}
	got, ok := store.Lookup(4)
	if !ok || !got.Acknowledged {
		t.Fatalf("Lookup() = (%+v, %v), want acknowledged message", got, ok)
	}
	if store.Acknowledge(99) {
		t.Fatal("Acknowledge() found unknown message")
	}
}

func TestRemove(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	store := newTestStore(t, 1, time.Minute, clock)
	_, _ = store.Put(SentMessage{MessageID: 4, SequenceNumber: 3})
	got, ok := store.Remove(4)
	if !ok || got.MessageID != 4 || got.SequenceNumber != 3 {
		t.Fatalf("Remove() = (%+v, %v)", got, ok)
	}
	if store.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", store.Len())
	}
	if _, ok := store.Remove(4); ok {
		t.Fatal("second Remove() found message")
	}
}

func TestTTLExpiryIsStrictAndSynchronous(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	store := newTestStore(t, 2, 10*time.Second, clock)
	_, _ = store.Put(SentMessage{MessageID: 1})
	clock.Advance(9 * time.Second)
	if store.Len() != 1 {
		t.Fatalf("Len() before expiry = %d, want 1", store.Len())
	}
	clock.Advance(time.Second)
	if _, ok := store.Lookup(1); ok {
		t.Fatal("Lookup() found message at expiry boundary")
	}
	if store.Len() != 0 {
		t.Fatalf("Len() after expiry = %d, want 0", store.Len())
	}

	if _, err := store.Put(SentMessage{MessageID: 2, SentAt: clock.Now().Add(-10 * time.Second)}); !errors.Is(err, ErrExpired) {
		t.Fatalf("Put(expired) error = %v, want %v", err, ErrExpired)
	}
	if store.Len() != 0 {
		t.Fatalf("expired Put changed Len() to %d", store.Len())
	}
}

func TestExpireReturnsRemovedCount(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	store := newTestStore(t, 3, 10*time.Second, clock)
	_, _ = store.Put(SentMessage{MessageID: 1, SentAt: clock.Now()})
	_, _ = store.Put(SentMessage{MessageID: 2, SentAt: clock.Now().Add(5 * time.Second)})
	clock.Advance(10 * time.Second)
	if got := store.Expire(); got != 1 {
		t.Fatalf("Expire() = %d, want 1", got)
	}
	if store.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", store.Len())
	}
}

func TestCapacityEvictsEarliestExpiry(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	store := newTestStore(t, 2, time.Minute, clock)
	_, _ = store.Put(SentMessage{MessageID: 1, SentAt: clock.Now().Add(10 * time.Second)})
	_, _ = store.Put(SentMessage{MessageID: 2, SentAt: clock.Now()})
	evicted, err := store.Put(SentMessage{MessageID: 3, SentAt: clock.Now().Add(20 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if evicted == nil || evicted.MessageID != 2 {
		t.Fatalf("evicted = %+v, want message 2", evicted)
	}
	if store.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", store.Len())
	}
}

func TestCapacityTieUsesInsertionOrder(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	store := newTestStore(t, 2, time.Minute, clock)
	_, _ = store.Put(SentMessage{MessageID: 1})
	_, _ = store.Put(SentMessage{MessageID: 2})
	evicted, _ := store.Put(SentMessage{MessageID: 3})
	if evicted == nil || evicted.MessageID != 1 {
		t.Fatalf("evicted = %+v, want message 1", evicted)
	}
}

func TestReplacingMessageDoesNotConsumeCapacity(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	store := newTestStore(t, 1, time.Minute, clock)
	_, _ = store.Put(SentMessage{MessageID: 1, Payload: []byte("old")})
	evicted, err := store.Put(SentMessage{MessageID: 1, Payload: []byte("new")})
	if err != nil || evicted != nil {
		t.Fatalf("replacement Put() = (%+v, %v)", evicted, err)
	}
	got, _ := store.Lookup(1)
	if string(got.Payload) != "new" || store.Len() != 1 {
		t.Fatalf("replacement = %+v, len = %d", got, store.Len())
	}
}

func TestStoreConcurrentAccess(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	const capacity = 64
	store := newTestStore(t, capacity, time.Hour, clock)

	var workers sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		worker := worker
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := 0; i < 500; i++ {
				id := int64(worker*500 + i)
				payload := []byte{byte(worker), byte(i)}
				if _, err := store.Put(SentMessage{MessageID: id, SequenceNumber: int32(i), Payload: payload}); err != nil {
					t.Errorf("Put(%d) error = %v", id, err)
					return
				}
				store.Lookup(id)
				store.LookupForResend(id)
				if i%2 == 0 {
					store.Acknowledge(id)
				}
				if i%3 == 0 {
					store.Remove(id)
				}
				if got := store.Len(); got > capacity {
					t.Errorf("Len() = %d, exceeds capacity %d", got, capacity)
					return
				}
			}
		}()
	}
	workers.Wait()
	if got := store.Len(); got > capacity {
		t.Fatalf("final Len() = %d, exceeds capacity %d", got, capacity)
	}
}
