package runtime

import (
	"context"
	"errors"
	"sync"

	"github.com/r6m/tlrpc/session"
)

var (
	ErrSessionLeaseReplaced = errors.New("runtime: session lease replaced by reconnect")
	ErrSessionLeaseReleased = errors.New("runtime: session lease released")
	ErrSessionLeaseInactive = errors.New("runtime: session lease is not active")
)

// SessionLeaseRegistry guarantees one active runtime owner for each composite
// MTProto session. A reconnect cancels the previous owner and waits for it to
// release before loading durable state for the replacement.
type SessionLeaseRegistry struct {
	store session.Store
	mu    sync.Mutex
	items map[session.SessionKey]*SessionLease
}

func NewSessionLeaseRegistry(store session.Store) *SessionLeaseRegistry {
	if store == nil {
		panic("runtime: nil session store")
	}
	return &SessionLeaseRegistry{store: store, items: make(map[session.SessionKey]*SessionLease)}
}

func (r *SessionLeaseRegistry) Acquire(ctx context.Context, key session.SessionKey, initial session.Snapshot) (*SessionLease, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		r.mu.Lock()
		previous := r.items[key]
		if previous == nil {
			leaseCtx, cancel := context.WithCancelCause(ctx)
			lease := &SessionLease{
				registry: r,
				key:      key,
				ctx:      leaseCtx,
				cancel:   cancel,
				done:     make(chan struct{}),
			}
			r.items[key] = lease
			r.mu.Unlock()

			snapshot, created, err := r.store.LoadOrCreate(ctx, key, initial)
			if err != nil {
				lease.Release()
				return nil, err
			}
			lease.opMu.Lock()
			lease.snapshot = snapshot.Clone()
			lease.created = created
			lease.ready = true
			lease.opMu.Unlock()
			return lease, nil
		}
		done := previous.Done()
		r.mu.Unlock()

		previous.Retire(ErrSessionLeaseReplaced)
		select {
		case <-done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// SessionLease is the exclusive mutable owner of one durable session snapshot.
type SessionLease struct {
	registry *SessionLeaseRegistry
	key      session.SessionKey
	ctx      context.Context
	cancel   context.CancelCauseFunc
	done     chan struct{}
	once     sync.Once
	opMu     sync.Mutex
	snapshot session.Snapshot
	created  bool
	ready    bool
	released bool
}

func (l *SessionLease) Key() session.SessionKey  { return l.key }
func (l *SessionLease) Context() context.Context { return l.ctx }
func (l *SessionLease) Done() <-chan struct{}    { return l.done }

// Created reports whether Acquire atomically inserted the initial durable
// snapshot rather than loading an existing session.
func (l *SessionLease) Created() bool {
	l.opMu.Lock()
	defer l.opMu.Unlock()
	return l.ready && !l.released && l.created
}

func (l *SessionLease) Snapshot() (session.Snapshot, error) {
	l.opMu.Lock()
	defer l.opMu.Unlock()
	if !l.ready || l.released {
		return session.Snapshot{}, ErrSessionLeaseInactive
	}
	return l.snapshot.Clone(), nil
}

// Commit durably saves next before exposing it as the lease's current state.
// Retire is serialized with Commit, so a replacement cannot overlap a stale
// save with its own session load.
func (l *SessionLease) Commit(ctx context.Context, next session.Snapshot) error {
	if next.Key() != l.key {
		return session.ErrSessionKeyMismatch
	}
	if ctx == nil {
		ctx = context.Background()
	}
	l.opMu.Lock()
	defer l.opMu.Unlock()
	if !l.ready || l.released {
		return ErrSessionLeaseInactive
	}
	if err := context.Cause(l.ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	commitCtx, cancel := context.WithCancel(l.ctx)
	stop := context.AfterFunc(ctx, cancel)
	defer func() {
		stop()
		cancel()
	}()
	if err := l.registry.store.Save(commitCtx, l.key, next); err != nil {
		return err
	}
	l.snapshot = next.Clone()
	return nil
}

// Delete removes the durable session while this lease is still its exclusive
// owner. The lease is retired after deletion and must then be released.
func (l *SessionLease) Delete(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	l.opMu.Lock()
	defer l.opMu.Unlock()
	if !l.ready || l.released {
		return ErrSessionLeaseInactive
	}
	if err := context.Cause(l.ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := l.registry.store.Delete(ctx, l.key); err != nil {
		return err
	}
	l.ready = false
	l.cancel(ErrSessionLeaseReleased)
	return nil
}

func (l *SessionLease) Retire(cause error) {
	if cause == nil {
		cause = ErrSessionLeaseInactive
	}
	l.opMu.Lock()
	l.cancel(cause)
	l.opMu.Unlock()
}

func (l *SessionLease) Release() {
	l.once.Do(func() {
		l.opMu.Lock()
		l.released = true
		l.cancel(ErrSessionLeaseReleased)
		l.opMu.Unlock()

		l.registry.mu.Lock()
		if l.registry.items[l.key] == l {
			delete(l.registry.items, l.key)
		}
		l.registry.mu.Unlock()
		close(l.done)
	})
}
