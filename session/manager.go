package session

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/r6m/tlrpc/crypto"
)

var ErrSessionNotFound = errors.New("session: not found")

// Manager manages session storage.
type Manager interface {
	Get(authKeyID crypto.KeyID) (*Session, error)
	Create(authKeyID crypto.KeyID) (*Session, error)
	Save(session *Session) error
	Delete(authKeyID crypto.KeyID) error
	GC(maxAge time.Duration)
}

// MemoryManager stores sessions in memory.
type MemoryManager struct {
	mu       sync.RWMutex
	sessions map[crypto.KeyID]*Session
	capacity int
}

// NewMemoryManager creates a new in-memory session manager.
func NewMemoryManager() *MemoryManager {
	return &MemoryManager{
		sessions: make(map[crypto.KeyID]*Session),
		capacity: 1024,
	}
}

// Get returns the session for the given auth key.
func (m *MemoryManager) Get(authKeyID crypto.KeyID) (*Session, error) {
	m.mu.RLock()
	session, ok := m.sessions[authKeyID]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrSessionNotFound
	}
	return session, nil
}

// Create creates and stores a new session.
func (m *MemoryManager) Create(authKeyID crypto.KeyID) (*Session, error) {
	session := &Session{
		ID:           newSessionID(),
		AuthKeyID:    authKeyID,
		CreatedAt:    time.Now().UTC(),
		LastActivity: time.Now().UTC(),
		RecentMsgIDs: NewLRUCache(m.capacity),
	}
	m.mu.Lock()
	m.sessions[authKeyID] = session
	m.mu.Unlock()
	return session, nil
}

// Save persists a session (no-op for memory manager).
func (m *MemoryManager) Save(session *Session) error {
	if session == nil {
		return nil
	}
	m.mu.Lock()
	m.sessions[session.AuthKeyID] = session
	m.mu.Unlock()
	return nil
}

// Delete removes a session.
func (m *MemoryManager) Delete(authKeyID crypto.KeyID) error {
	m.mu.Lock()
	delete(m.sessions, authKeyID)
	m.mu.Unlock()
	return nil
}

// GC removes sessions inactive longer than maxAge.
func (m *MemoryManager) GC(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	m.mu.Lock()
	for key, session := range m.sessions {
		if session.LastActivity.Before(cutoff) {
			delete(m.sessions, key)
		}
	}
	m.mu.Unlock()
}

func newSessionID() int64 {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return time.Now().UnixNano()
	}
	return int64(binary.LittleEndian.Uint64(buf[:]))
}
