package session

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrLeaseLost                = errors.New("session: lease ownership lost")
	ErrLeaseReleased            = errors.New("session: lease released")
	ErrLeaseInactive            = errors.New("session: lease is not active")
	ErrLeaseGenerationExhausted = errors.New("session: lease generation exhausted")
)

// Coordinator owns exclusive protocol-session ownership. Implementations may be
// process-local or backed by external coordination, but every successful
// acquisition must return a lease with a generation greater than every previous
// lease for the same key.
type Coordinator interface {
	Acquire(ctx context.Context, key SessionKey, initial Snapshot) (Lease, error)
}

// Lease is the exclusive mutable owner of one protocol session snapshot.
//
// Context is canceled when ownership is lost, the lease is released, or the
// runtime retires the lease after a fatal protocol/session error. Save and
// Delete must be fenced by Key and Generation so a stale owner cannot mutate
// durable state after another owner has taken over.
type Lease interface {
	Key() SessionKey
	Generation() int64
	Context() context.Context
	Done() <-chan struct{}
	Created() bool
	Snapshot() (Snapshot, error)
	Save(ctx context.Context, next Snapshot) error
	Delete(ctx context.Context) error
	Retire(cause error)
	Release()
}

// LocalCoordinator coordinates leases in the current process. It is suitable
// for tests and single-process development. Multi-process deployments should
// provide their own Coordinator backed by a durable fencing primitive.
type LocalCoordinator struct {
	store       Store
	mu          sync.Mutex
	leases      map[SessionKey]*localLease
	generations map[SessionKey]int64
}

var _ Coordinator = (*LocalCoordinator)(nil)

func NewLocalCoordinator(store Store) *LocalCoordinator {
	if store == nil {
		panic("session: nil store")
	}
	return &LocalCoordinator{
		store:       store,
		leases:      make(map[SessionKey]*localLease),
		generations: make(map[SessionKey]int64),
	}
}

func (c *LocalCoordinator) Acquire(ctx context.Context, key SessionKey, initial Snapshot) (Lease, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if initial.Key() != key {
		return nil, ErrSessionKeyMismatch
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		c.mu.Lock()
		previous := c.leases[key]
		if previous == nil {
			generation := c.generations[key] + 1
			if generation <= c.generations[key] {
				c.mu.Unlock()
				return nil, ErrLeaseGenerationExhausted
			}
			leaseCtx, cancel := context.WithCancelCause(ctx)
			lease := &localLease{
				coordinator: c,
				key:         key,
				generation:  generation,
				ctx:         leaseCtx,
				cancel:      cancel,
				done:        make(chan struct{}),
			}
			c.generations[key] = generation
			c.leases[key] = lease
			c.mu.Unlock()

			snapshot, created, err := c.store.LoadOrCreate(ctx, key, initial)
			if err != nil {
				lease.Release()
				return nil, err
			}
			if err := context.Cause(leaseCtx); err != nil {
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
		c.mu.Unlock()

		previous.Retire(ErrLeaseLost)
		select {
		case <-done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

type localLease struct {
	coordinator *LocalCoordinator
	key         SessionKey
	generation  int64
	ctx         context.Context
	cancel      context.CancelCauseFunc
	done        chan struct{}
	once        sync.Once
	opMu        sync.Mutex
	snapshot    Snapshot
	created     bool
	ready       bool
	released    bool
}

var _ Lease = (*localLease)(nil)

func (l *localLease) Key() SessionKey          { return l.key }
func (l *localLease) Generation() int64        { return l.generation }
func (l *localLease) Context() context.Context { return l.ctx }
func (l *localLease) Done() <-chan struct{}    { return l.done }

// Created reports whether Acquire atomically inserted the initial durable
// snapshot rather than loading an existing session.
func (l *localLease) Created() bool {
	l.opMu.Lock()
	defer l.opMu.Unlock()
	return l.ready && !l.released && l.created
}

func (l *localLease) Snapshot() (Snapshot, error) {
	l.opMu.Lock()
	defer l.opMu.Unlock()
	if err := l.ensureActiveLocked(); err != nil {
		return Snapshot{}, err
	}
	return l.snapshot.Clone(), nil
}

func (l *localLease) Save(ctx context.Context, next Snapshot) error {
	if next.Key() != l.key {
		return ErrSessionKeyMismatch
	}
	if ctx == nil {
		ctx = context.Background()
	}
	l.opMu.Lock()
	defer l.opMu.Unlock()
	if err := l.ensureActiveLocked(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	saveCtx, cancel := context.WithCancel(l.ctx)
	stop := context.AfterFunc(ctx, cancel)
	defer func() {
		stop()
		cancel()
	}()
	if err := l.coordinator.store.Save(saveCtx, l.key, next); err != nil {
		return err
	}
	l.snapshot = next.Clone()
	return nil
}

func (l *localLease) Delete(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	l.opMu.Lock()
	defer l.opMu.Unlock()
	if err := l.ensureActiveLocked(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	deleteCtx, cancel := context.WithCancel(l.ctx)
	stop := context.AfterFunc(ctx, cancel)
	defer func() {
		stop()
		cancel()
	}()
	if err := l.coordinator.store.Delete(deleteCtx, l.key); err != nil {
		return err
	}
	l.ready = false
	l.cancel(ErrLeaseReleased)
	return nil
}

func (l *localLease) Retire(cause error) {
	if cause == nil {
		cause = ErrLeaseInactive
	}
	l.opMu.Lock()
	l.cancel(cause)
	l.opMu.Unlock()
}

func (l *localLease) Release() {
	l.once.Do(func() {
		l.opMu.Lock()
		l.released = true
		l.cancel(ErrLeaseReleased)
		l.opMu.Unlock()

		l.coordinator.mu.Lock()
		if l.coordinator.leases[l.key] == l {
			delete(l.coordinator.leases, l.key)
		}
		l.coordinator.mu.Unlock()
		close(l.done)
	})
}

func (l *localLease) ensureActiveLocked() error {
	if !l.ready || l.released {
		return ErrLeaseInactive
	}
	if err := context.Cause(l.ctx); err != nil {
		return err
	}
	l.coordinator.mu.Lock()
	current := l.coordinator.leases[l.key]
	l.coordinator.mu.Unlock()
	if current != l {
		return ErrLeaseLost
	}
	return nil
}
