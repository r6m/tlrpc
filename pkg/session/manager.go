// Package session provides session management.
package session

import (
	"sync"
	"time"
)

// Manager manages sessions.
type Manager interface {
	Get(authKeyID int64) (*Session, error)
	Save(session *Session) error
	Delete(authKeyID int64) error
}

// Store is the session storage interface.
type Store interface {
	Get(authKeyID int64) (*Session, error)
	Save(session *Session) error
	Delete(authKeyID int64) error
}

// managerImpl implements Manager.
type managerImpl struct {
	store Store
	mu    sync.RWMutex
}

// NewManager creates a new session manager.
func NewManager(store Store) Manager {
	return &managerImpl{
		store: store,
	}
}

// Get retrieves a session.
func (m *managerImpl) Get(authKeyID int64) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.store.Get(authKeyID)
}

// Save saves a session.
func (m *managerImpl) Save(session *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session.UpdateTimestamp()
	return m.store.Save(session)
}

// Delete deletes a session.
func (m *managerImpl) Delete(authKeyID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.store.Delete(authKeyID)
}

// cleanup removes expired sessions.
func (m *managerImpl) cleanup() {
	// TODO: Implement session cleanup
}

// startCleanup starts the cleanup goroutine.
func (m *managerImpl) startCleanup(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			m.cleanup()
		}
	}()
}