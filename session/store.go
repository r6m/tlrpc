package session

import (
	"context"
	"sync"
)

// Store is the final Runtime v2 durable protocol-session contract. Values cross
// the boundary as detached snapshots; stores never retain mutable runtime
// session pointers.
type Store interface {
	Load(ctx context.Context, key SessionKey) (Snapshot, error)
	LoadOrCreate(ctx context.Context, key SessionKey, initial Snapshot) (snapshot Snapshot, created bool, err error)
	Save(ctx context.Context, key SessionKey, snapshot Snapshot) error
	Delete(ctx context.Context, key SessionKey) error
}

// MemoryStore is a detached-snapshot Store suitable for tests and single-node
// development. Production applications provide their own durable Store.
type MemoryStore struct {
	mu        sync.RWMutex
	snapshots map[SessionKey]Snapshot
}

var _ Store = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{snapshots: make(map[SessionKey]Snapshot)}
}

func (s *MemoryStore) Load(ctx context.Context, key SessionKey) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	s.mu.RLock()
	snapshot, ok := s.snapshots[key]
	s.mu.RUnlock()
	if !ok {
		return Snapshot{}, ErrSessionNotFound
	}
	return snapshot.Clone(), nil
}

func (s *MemoryStore) LoadOrCreate(ctx context.Context, key SessionKey, initial Snapshot) (Snapshot, bool, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, err
	}
	if initial.Key() != key {
		return Snapshot{}, false, ErrSessionKeyMismatch
	}
	s.mu.Lock()
	if snapshot, ok := s.snapshots[key]; ok {
		s.mu.Unlock()
		return snapshot.Clone(), false, nil
	}
	stored := initial.Clone()
	s.snapshots[key] = stored
	s.mu.Unlock()
	return stored.Clone(), true, nil
}

func (s *MemoryStore) Save(ctx context.Context, key SessionKey, snapshot Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if snapshot.Key() != key {
		return ErrSessionKeyMismatch
	}
	s.mu.Lock()
	s.snapshots[key] = snapshot.Clone()
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) Delete(ctx context.Context, key SessionKey) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.snapshots, key)
	s.mu.Unlock()
	return nil
}
