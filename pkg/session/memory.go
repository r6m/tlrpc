// Package session provides in-memory session storage.
package session

import (
	"errors"
	"sync"
)

// MemoryStore implements Store using in-memory storage.
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[int64]*Session
}

// NewMemoryStore creates a new in-memory session store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: make(map[int64]*Session),
	}
}

// Get retrieves a session by auth key ID.
func (s *MemoryStore) Get(authKeyID int64) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[authKeyID]
	if !exists {
		return nil, ErrSessionNotFound
	}

	return session, nil
}

// Save saves a session.
func (s *MemoryStore) Save(session *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[session.AuthKeyID] = session
	return nil
}

// Delete deletes a session.
func (s *MemoryStore) Delete(authKeyID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[authKeyID]; !exists {
		return ErrSessionNotFound
	}

	delete(s.sessions, authKeyID)
	return nil
}

// GetAll returns all sessions (for debugging).
func (s *MemoryStore) GetAll() map[int64]*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[int64]*Session, len(s.sessions))
	for k, v := range s.sessions {
		result[k] = v
	}
	return result
}

// Clear removes all sessions.
func (s *MemoryStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions = make(map[int64]*Session)
}

// NewMemoryManager creates a new session manager with memory store.
func NewMemoryManager() Manager {
	return NewManager(NewMemoryStore())
}

// Errors
var (
	ErrSessionNotFound = errors.New("session: not found")
)