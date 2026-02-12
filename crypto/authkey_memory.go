package crypto

import "sync"

// MemoryAuthKeyManager is an in-memory auth key store.
type MemoryAuthKeyManager struct {
	mu   sync.RWMutex
	keys map[KeyID]AuthKey
}

// NewMemoryAuthKeyManager creates a new in-memory auth key manager.
func NewMemoryAuthKeyManager() *MemoryAuthKeyManager {
	return &MemoryAuthKeyManager{keys: make(map[KeyID]AuthKey)}
}

// Get returns the auth key for the given key ID.
func (m *MemoryAuthKeyManager) Get(keyID KeyID) (AuthKey, error) {
	m.mu.RLock()
	key, ok := m.keys[keyID]
	m.mu.RUnlock()
	if !ok {
		return AuthKey{}, ErrAuthKeyNotFound
	}
	return key, nil
}

// Put stores the auth key for the given key ID.
func (m *MemoryAuthKeyManager) Put(keyID KeyID, key AuthKey) error {
	m.mu.Lock()
	m.keys[keyID] = key
	m.mu.Unlock()
	return nil
}

// Delete removes the auth key for the given key ID.
func (m *MemoryAuthKeyManager) Delete(keyID KeyID) error {
	m.mu.Lock()
	delete(m.keys, keyID)
	m.mu.Unlock()
	return nil
}
