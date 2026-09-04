package runtime

import (
	"errors"
	"sync"
	"time"

	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
)

var (
	// ErrInvalidInboundStateCapacity reports a ledger configured without space.
	ErrInvalidInboundStateCapacity = errors.New("runtime: inbound state capacity must be positive")
	// ErrInvalidInboundStateTTL reports a ledger configured without a retention period.
	ErrInvalidInboundStateTTL = errors.New("runtime: inbound state TTL must be positive")
)

// InboundStateConfig defines the hard resource bounds for an InboundStateLedger.
type InboundStateConfig struct {
	// Capacity is the maximum number of inbound message states retained at once.
	Capacity int
	// TTL is measured from the most recent recording of a message ID.
	TTL time.Duration
	// Now supplies the current time. It defaults to time.Now when nil.
	Now func() time.Time
}

// InboundStateLedger retains canonical MTProto state information for validated
// inbound messages. All methods are safe for concurrent use.
type InboundStateLedger struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	now      func() time.Time
	order    uint64
	entries  map[int64]inboundStateEntry
}

type inboundStateEntry struct {
	status    byte
	expiresAt time.Time
	order     uint64
}

// NewInboundStateLedger constructs an empty, bounded inbound state ledger.
func NewInboundStateLedger(config InboundStateConfig) (*InboundStateLedger, error) {
	if config.Capacity <= 0 {
		return nil, ErrInvalidInboundStateCapacity
	}
	if config.TTL <= 0 {
		return nil, ErrInvalidInboundStateTTL
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &InboundStateLedger{
		capacity: config.Capacity,
		ttl:      config.TTL,
		now:      now,
		entries:  make(map[int64]inboundStateEntry, config.Capacity),
	}, nil
}

// Record retains one protocol-validated inbound message. Re-recording an ID
// refreshes its TTL and makes it the newest entry for deterministic eviction.
func (l *InboundStateLedger) Record(message InboundMessage) error {
	if err := message.Validate(); err != nil {
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.expireLocked(now)
	status := byte(mtprototl.MessageStateReceived | mtprototl.MessageStateKnown)
	if !message.ContentRelated {
		status |= mtprototl.MessageStateNoAcknowledgement
	}
	l.order++
	l.entries[message.MessageID] = inboundStateEntry{
		status:    status,
		expiresAt: now.Add(l.ttl),
		order:     l.order,
	}
	l.evictLocked()
	return nil
}

// Complete marks retained IDs as processed and optionally acknowledged and/or
// responded to. Unknown and expired IDs are idempotent no-ops.
func (l *InboundStateLedger) Complete(messageIDs []int64, acknowledged, responded bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.expireLocked(l.now())
	for _, messageID := range messageIDs {
		entry, ok := l.entries[messageID]
		if !ok {
			continue
		}
		entry.status |= mtprototl.MessageStateProcessing
		if acknowledged {
			entry.status |= mtprototl.MessageStateAcknowledged
		}
		if responded {
			entry.status |= mtprototl.MessageStateResponseGenerated
		}
		l.entries[messageID] = entry
	}
}

// StateInfo returns one canonical MTProto state byte per requested ID, in the
// same order as messageIDs. The returned slice is detached from ledger state.
func (l *InboundStateLedger) StateInfo(messageIDs []int64) []byte {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.expireLocked(l.now())
	var minID, maxID int64
	haveRange := false
	for messageID := range l.entries {
		if !haveRange || messageID < minID {
			minID = messageID
		}
		if !haveRange || messageID > maxID {
			maxID = messageID
		}
		haveRange = true
	}

	info := make([]byte, len(messageIDs))
	for i, messageID := range messageIDs {
		if entry, ok := l.entries[messageID]; ok {
			info[i] = entry.status
			continue
		}
		switch {
		case !haveRange || messageID < minID:
			info[i] = mtprototl.MessageStateUnknownTooOld
		case messageID > maxID:
			info[i] = mtprototl.MessageStateUnknownTooHigh
		default:
			info[i] = mtprototl.MessageStateNotReceived
		}
	}
	return info
}

// HasResponse reports whether this process still retains a generated response
// for an inbound message. It is deliberately process-local: retained outbound
// frames are not part of a durable session snapshot.
func (l *InboundStateLedger) HasResponse(messageID int64) bool {
	if l == nil || messageID == 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	l.expireLocked(l.now())
	entry, ok := l.entries[messageID]
	return ok && entry.status&mtprototl.MessageStateResponseGenerated != 0
}

// Expire removes entries whose TTL has elapsed and returns the number removed.
func (l *InboundStateLedger) Expire() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.expireLocked(l.now())
}

// Len returns the number of retained, non-expired message states.
func (l *InboundStateLedger) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.expireLocked(l.now())
	return len(l.entries)
}

func (l *InboundStateLedger) evictLocked() {
	for len(l.entries) > l.capacity {
		var (
			oldestID    int64
			oldestEntry inboundStateEntry
			haveOldest  bool
		)
		for messageID, entry := range l.entries {
			if !haveOldest || entry.expiresAt.Before(oldestEntry.expiresAt) ||
				(entry.expiresAt.Equal(oldestEntry.expiresAt) && entry.order < oldestEntry.order) {
				oldestID = messageID
				oldestEntry = entry
				haveOldest = true
			}
		}
		delete(l.entries, oldestID)
	}
}

func (l *InboundStateLedger) expireLocked(now time.Time) int {
	removed := 0
	for messageID, entry := range l.entries {
		if !entry.expiresAt.After(now) {
			delete(l.entries, messageID)
			removed++
		}
	}
	return removed
}
