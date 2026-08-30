package runtime

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/r6m/tlrpc/mtproto/reliability"
	"github.com/r6m/tlrpc/session"
)

var (
	// ErrInvalidReliabilitySessionCapacity reports a registry configured
	// without space for any session.
	ErrInvalidReliabilitySessionCapacity = errors.New("runtime: reliability session capacity must be positive")
	// ErrInvalidReliabilityMessageCapacity reports a registry configured
	// without space for per-session message state.
	ErrInvalidReliabilityMessageCapacity = errors.New("runtime: reliability message capacity must be positive")
	// ErrInvalidReliabilityTTL reports a registry configured without an idle
	// and message retention period.
	ErrInvalidReliabilityTTL = errors.New("runtime: reliability TTL must be positive")
)

// ReliabilityCapacityError reports that a new session cannot be admitted
// because every retained registry entry is active.
type ReliabilityCapacityError struct {
	MaxSessions int
}

func (e *ReliabilityCapacityError) Error() string {
	return fmt.Sprintf("runtime: reliability session capacity exhausted (max %d)", e.MaxSessions)
}

// ReliabilityRegistryConfig defines the resource bounds shared by all
// session-scoped reliability state owned by a registry.
type ReliabilityRegistryConfig struct {
	MaxSessions     int
	MessageCapacity int
	TTL             time.Duration
	Now             func() time.Time
}

// ReliabilityRegistry owns bounded inbound and outbound reliability state by
// complete MTProto session identity. It performs lifecycle cleanup during
// Acquire and does not start a background goroutine.
type ReliabilityRegistry struct {
	mu              sync.Mutex
	maxSessions     int
	messageCapacity int
	ttl             time.Duration
	now             func() time.Time
	order           uint64
	entries         map[session.SessionKey]*reliabilityRegistryEntry
}

type reliabilityRegistryEntry struct {
	outbound  *reliability.Store
	inbound   *InboundStateLedger
	refs      int
	idleAt    time.Time
	idleOrder uint64
}

// ReliabilityHandle is an opaque, reference-counted acquisition of one
// session's reliability state. Release is safe to call more than once.
type ReliabilityHandle struct {
	registry    *ReliabilityRegistry
	key         session.SessionKey
	entry       *reliabilityRegistryEntry
	releaseOnce sync.Once
}

// NewReliabilityRegistry constructs an empty session-scoped reliability
// registry with strict session, message, and time bounds.
func NewReliabilityRegistry(config ReliabilityRegistryConfig) (*ReliabilityRegistry, error) {
	if config.MaxSessions <= 0 {
		return nil, ErrInvalidReliabilitySessionCapacity
	}
	if config.MessageCapacity <= 0 {
		return nil, ErrInvalidReliabilityMessageCapacity
	}
	if config.TTL <= 0 {
		return nil, ErrInvalidReliabilityTTL
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &ReliabilityRegistry{
		maxSessions:     config.MaxSessions,
		messageCapacity: config.MessageCapacity,
		ttl:             config.TTL,
		now:             now,
		entries:         make(map[session.SessionKey]*reliabilityRegistryEntry, config.MaxSessions),
	}, nil
}

// Acquire retains the exact reliability state associated with key. Concurrent
// and reconnecting acquisitions of the same live session share both stores.
func (r *ReliabilityRegistry) Acquire(key session.SessionKey) (*ReliabilityHandle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	r.expireIdleLocked(now)
	if entry, ok := r.entries[key]; ok {
		entry.refs++
		return r.newHandle(key, entry), nil
	}
	if len(r.entries) == r.maxSessions && !r.evictOldestIdleLocked() {
		return nil, &ReliabilityCapacityError{MaxSessions: r.maxSessions}
	}

	outbound, err := reliability.New(reliability.Config{
		Capacity: r.messageCapacity,
		TTL:      r.ttl,
		Now:      r.now,
	})
	if err != nil {
		return nil, err
	}
	inbound, err := NewInboundStateLedger(InboundStateConfig{
		Capacity: r.messageCapacity,
		TTL:      r.ttl,
		Now:      r.now,
	})
	if err != nil {
		return nil, err
	}
	entry := &reliabilityRegistryEntry{
		outbound: outbound,
		inbound:  inbound,
		refs:     1,
	}
	r.entries[key] = entry
	return r.newHandle(key, entry), nil
}

func (r *ReliabilityRegistry) newHandle(key session.SessionKey, entry *reliabilityRegistryEntry) *ReliabilityHandle {
	return &ReliabilityHandle{registry: r, key: key, entry: entry}
}

// Release relinquishes this acquisition. The entry becomes eligible for idle
// expiration and deterministic eviction after its final handle is released.
func (h *ReliabilityHandle) Release() {
	if h == nil {
		return
	}
	h.releaseOnce.Do(func() {
		if h.registry != nil {
			h.registry.release(h.key, h.entry)
		}
	})
}

// outboundStore exposes the per-session store only inside Runtime v2, where it
// is passed to the session's Writer during construction.
func (h *ReliabilityHandle) outboundStore() *reliability.Store {
	if h == nil || h.entry == nil {
		return nil
	}
	return h.entry.outbound
}

// inboundLedger exposes semantic inbound state only to Runtime v2 connection
// and control handling.
func (h *ReliabilityHandle) inboundLedger() *InboundStateLedger {
	if h == nil || h.entry == nil {
		return nil
	}
	return h.entry.inbound
}

func (r *ReliabilityRegistry) release(key session.SessionKey, acquired *reliabilityRegistryEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.entries[key]
	if !ok || entry != acquired || entry.refs == 0 {
		return
	}
	entry.refs--
	if entry.refs != 0 {
		return
	}
	r.order++
	entry.idleAt = r.now()
	entry.idleOrder = r.order
}

func (r *ReliabilityRegistry) expireIdleLocked(now time.Time) {
	for key, entry := range r.entries {
		if entry.refs == 0 && !entry.idleAt.Add(r.ttl).After(now) {
			delete(r.entries, key)
		}
	}
}

func (r *ReliabilityRegistry) evictOldestIdleLocked() bool {
	var (
		candidateKey   session.SessionKey
		candidateEntry *reliabilityRegistryEntry
	)
	for key, entry := range r.entries {
		if entry.refs != 0 {
			continue
		}
		if candidateEntry == nil || entry.idleAt.Before(candidateEntry.idleAt) ||
			(entry.idleAt.Equal(candidateEntry.idleAt) && entry.idleOrder < candidateEntry.idleOrder) {
			candidateKey = key
			candidateEntry = entry
		}
	}
	if candidateEntry == nil {
		return false
	}
	delete(r.entries, candidateKey)
	return true
}
