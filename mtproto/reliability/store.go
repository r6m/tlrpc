package reliability

import (
	"container/heap"
	"errors"
	"sync"
	"time"
)

var (
	// ErrInvalidCapacity is returned when a store is configured without space.
	ErrInvalidCapacity = errors.New("mtproto reliability: capacity must be positive")
	// ErrInvalidTTL is returned when a store is configured with a non-positive TTL.
	ErrInvalidTTL = errors.New("mtproto reliability: TTL must be positive")
	// ErrExpired is returned when a message has already exceeded the store TTL.
	ErrExpired = errors.New("mtproto reliability: message has expired")
)

// Config defines the hard resource bounds for a Store.
type Config struct {
	// Capacity is the maximum number of messages retained at once.
	Capacity int
	// TTL is measured from each message's SentAt timestamp.
	TTL time.Duration
	// Now supplies the current time. It defaults to time.Now when nil.
	Now func() time.Time
}

// SentMessage is an immutable snapshot of one outbound MTProto message.
// Store copies Payload when a message enters or leaves the store.
type SentMessage struct {
	MessageID        int64
	RequestMessageID int64
	SequenceNumber   int32
	Payload          []byte
	SentAt           time.Time
	Acknowledged     bool
}

// Store tracks outbound messages with strict capacity and TTL bounds.
// All methods are safe for concurrent use.
type Store struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	now      func() time.Time
	order    uint64
	entries  map[int64]*entry
	expiry   expiryHeap
}

type entry struct {
	message   SentMessage
	expiresAt time.Time
	order     uint64
	index     int
}

type expiryHeap []*entry

func (h expiryHeap) Len() int { return len(h) }

func (h expiryHeap) Less(i, j int) bool {
	if h[i].expiresAt.Equal(h[j].expiresAt) {
		return h[i].order < h[j].order
	}
	return h[i].expiresAt.Before(h[j].expiresAt)
}

func (h expiryHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *expiryHeap) Push(value any) {
	item := value.(*entry)
	item.index = len(*h)
	*h = append(*h, item)
}

func (h *expiryHeap) Pop() any {
	old := *h
	last := len(old) - 1
	item := old[last]
	old[last] = nil
	item.index = -1
	*h = old[:last]
	return item
}

// New constructs an empty Store. Capacity and TTL must both be positive.
func New(config Config) (*Store, error) {
	if config.Capacity <= 0 {
		return nil, ErrInvalidCapacity
	}
	if config.TTL <= 0 {
		return nil, ErrInvalidTTL
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Store{
		capacity: config.Capacity,
		ttl:      config.TTL,
		now:      now,
		entries:  make(map[int64]*entry, config.Capacity),
		expiry:   make(expiryHeap, 0, config.Capacity),
	}, nil
}

// Put inserts or replaces a sent message. A zero SentAt is replaced with the
// current time. If insertion reaches capacity, the message expiring first is
// evicted and returned. Equal expiry times are resolved by insertion order.
func (s *Store) Put(message SentMessage) (*SentMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.expireLocked(now)
	if message.SentAt.IsZero() {
		message.SentAt = now
	}
	expiresAt := message.SentAt.Add(s.ttl)
	if !expiresAt.After(now) {
		return nil, ErrExpired
	}
	message.Payload = cloneBytes(message.Payload)

	s.order++
	if current, ok := s.entries[message.MessageID]; ok {
		current.message = message
		current.expiresAt = expiresAt
		current.order = s.order
		heap.Fix(&s.expiry, current.index)
		return nil, nil
	}

	var evicted *SentMessage
	if len(s.entries) == s.capacity {
		item := heap.Pop(&s.expiry).(*entry)
		delete(s.entries, item.message.MessageID)
		copy := cloneMessage(item.message)
		evicted = &copy
	}

	item := &entry{message: message, expiresAt: expiresAt, order: s.order}
	s.entries[message.MessageID] = item
	heap.Push(&s.expiry, item)
	return evicted, nil
}

// Acknowledge marks a retained message as acknowledged. It returns false when
// the message is unknown or expired.
func (s *Store) Acknowledge(messageID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.expireLocked(s.now())
	item, ok := s.entries[messageID]
	if !ok {
		return false
	}
	item.message.Acknowledged = true
	return true
}

// Lookup returns a snapshot of a retained message, including acknowledgement
// state. It returns false when the message is unknown or expired.
func (s *Store) Lookup(messageID int64) (SentMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.expireLocked(s.now())
	item, ok := s.entries[messageID]
	if !ok {
		return SentMessage{}, false
	}
	return cloneMessage(item.message), true
}

// LookupForResend returns an unacknowledged retained message. Acknowledged,
// unknown, and expired messages are not eligible for resend.
func (s *Store) LookupForResend(messageID int64) (SentMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.expireLocked(s.now())
	item, ok := s.entries[messageID]
	if !ok || item.message.Acknowledged {
		return SentMessage{}, false
	}
	return cloneMessage(item.message), true
}

// Remove deletes a retained message and returns its final snapshot.
func (s *Store) Remove(messageID int64) (SentMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.expireLocked(s.now())
	item, ok := s.entries[messageID]
	if !ok {
		return SentMessage{}, false
	}
	heap.Remove(&s.expiry, item.index)
	delete(s.entries, messageID)
	return cloneMessage(item.message), true
}

// Expire removes all messages whose TTL has elapsed and returns the number
// removed. Store access already performs this cleanup; Expire lets callers do
// it at lifecycle-appropriate points without a background goroutine.
func (s *Store) Expire() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.expireLocked(s.now())
}

// Len returns the number of currently retained, non-expired messages.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked(s.now())
	return len(s.entries)
}

func (s *Store) expireLocked(now time.Time) int {
	removed := 0
	for len(s.expiry) > 0 && !s.expiry[0].expiresAt.After(now) {
		item := heap.Pop(&s.expiry).(*entry)
		delete(s.entries, item.message.MessageID)
		removed++
	}
	return removed
}

func cloneMessage(message SentMessage) SentMessage {
	message.Payload = cloneBytes(message.Payload)
	return message
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	return append([]byte(nil), value...)
}

// LookupResponse finds the newest retained response correlated to a request.
// This uses the same capacity and TTL as outbound packet retention.
func (s *Store) LookupResponse(requestID int64) (SentMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked(s.now())
	var found *entry
	for _, item := range s.entries {
		if item.message.RequestMessageID == requestID && requestID != 0 && (found == nil || item.message.MessageID > found.message.MessageID) {
			found = item
		}
	}
	if found == nil {
		return SentMessage{}, false
	}
	return cloneMessage(found.message), true
}
