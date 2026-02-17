package session

import (
	"sync"
	"time"

	"github.com/r6m/tlrpc/crypto"
)

// Legacy types for backward compatibility

// SessionStore interface for session storage.
type SessionStore interface {
	Get(authKeyID int64) (*LegacySession, error)
	Save(session *LegacySession) error
	Delete(authKeyID int64) error
}

type LegacySession struct {
	ID        int64
	AuthKeyID int64
	Layer     int
	UserID    int64
	Data      map[string]interface{}
}

type sessionAdapter struct {
	store SessionStore
}

func NewSessionAdapter(store SessionStore) Manager {
	if store == nil {
		return NewMemoryManager()
	}
	return &sessionAdapter{store: store}
}

func (s *sessionAdapter) Get(authKeyID crypto.KeyID) (*Session, error) {
	legacy, err := s.store.Get(int64(authKeyID))
	if err != nil {
		return nil, err
	}
	sess := &Session{
		ID:        legacy.ID,
		AuthKeyID: authKeyID,
		Layer:     legacy.Layer,
		UserID:    legacy.UserID,
		Data:      sync.Map{},
	}
	syncMapFromLegacy(&sess.Data, legacy.Data)
	return sess, nil
}

func (s *sessionAdapter) Create(authKeyID crypto.KeyID) (*Session, error) {
	sess := &Session{
		ID:        time.Now().UnixNano(),
		AuthKeyID: authKeyID,
		Layer:     0,
		UserID:    0,
	}
	legacy := legacySessionFrom(sess)
	if err := s.store.Save(legacy); err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *sessionAdapter) Save(sess *Session) error {
	return s.store.Save(legacySessionFrom(sess))
}

func (s *sessionAdapter) Delete(authKeyID crypto.KeyID) error {
	return s.store.Delete(int64(authKeyID))
}

func (s *sessionAdapter) GC(maxAge time.Duration) {
}

func legacySessionFrom(sess *Session) *LegacySession {
	legacy := &LegacySession{
		ID:        sess.ID,
		AuthKeyID: int64(sess.AuthKeyID),
		Layer:     sess.Layer,
		UserID:    sess.UserID,
		Data:      map[string]interface{}{},
	}
	sess.Data.Range(func(key, value interface{}) bool {
		if k, ok := key.(string); ok {
			legacy.Data[k] = value
		}
		return true
	})
	return legacy
}

func syncMapFromLegacy(m *sync.Map, data map[string]interface{}) {
	for k, v := range data {
		m.Store(k, v)
	}
}
