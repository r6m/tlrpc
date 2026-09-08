package crypto

import (
	"sync"
	"time"
)

// MemoryAuthKeyManager is an in-memory auth key store.
type MemoryAuthKeyManager struct {
	mu        sync.RWMutex
	keys      map[KeyID]AuthKey
	temporary map[KeyID]TemporaryAuthKeyInfo
}

// NewMemoryAuthKeyManager creates a new in-memory auth key manager.
func NewMemoryAuthKeyManager() *MemoryAuthKeyManager {
	return &MemoryAuthKeyManager{keys: make(map[KeyID]AuthKey), temporary: make(map[KeyID]TemporaryAuthKeyInfo)}
}

// Get returns the auth key for the given key ID.
func (m *MemoryAuthKeyManager) Get(keyID KeyID) (AuthKey, error) {
	m.mu.RLock()
	key, ok := m.keys[keyID]
	if info, temporary := m.temporary[keyID]; temporary {
		ok = ok && time.Now().Before(info.ExpiresAt)
		if info.PermanentKeyID != 0 {
			_, parentPresent := m.keys[info.PermanentKeyID]
			ok = ok && parentPresent
		}
	}
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
	delete(m.temporary, keyID)
	for child, info := range m.temporary {
		if info.PermanentKeyID == keyID {
			delete(m.temporary, child)
			delete(m.keys, child)
		}
	}
	m.mu.Unlock()
	return nil
}

func (m *MemoryAuthKeyManager) PutTemporary(id KeyID, key AuthKey, expires time.Time) error {
	if !expires.After(time.Now()) || expires.After(time.Now().Add(MaxTemporaryAuthKeyLifetime)) || key.ID() != id {
		return ErrTemporaryKeyExpiry
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, info := range m.temporary {
		if !time.Now().Before(info.ExpiresAt) {
			delete(m.temporary, id)
			delete(m.keys, id)
		}
	}
	if len(m.temporary) >= 4096 {
		return ErrTemporaryKeyExpiry
	}
	if _, exists := m.keys[id]; exists {
		return ErrInvalidKeyBinding
	}
	m.keys[id] = key
	m.temporary[id] = TemporaryAuthKeyInfo{ExpiresAt: expires}
	m.expireTemporary(id, expires)
	return nil
}

func (m *MemoryAuthKeyManager) expireTemporary(id KeyID, expires time.Time) {
	time.AfterFunc(time.Until(expires), func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if info, ok := m.temporary[id]; ok && !time.Now().Before(info.ExpiresAt) {
			delete(m.temporary, id)
			delete(m.keys, id)
		}
	})
}

func (m *MemoryAuthKeyManager) TemporaryInfo(id KeyID) (TemporaryAuthKeyInfo, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	info, ok := m.temporary[id]
	if ok && !time.Now().Before(info.ExpiresAt) {
		return info, true, ErrAuthKeyNotFound
	}
	return info, ok, nil
}

func (m *MemoryAuthKeyManager) BindTemporary(id, parent KeyID, expires time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.temporary[id]
	if !ok {
		return ErrAuthKeyNotFound
	}
	if _, ok := m.keys[parent]; !ok {
		return ErrAuthKeyNotFound
	}
	if _, ok := m.temporary[parent]; ok || parent == id {
		return ErrInvalidKeyBinding
	}
	if info.PermanentKeyID != 0 && info.PermanentKeyID != parent {
		return ErrTemporaryKeyAlreadyBound
	}
	if !time.Now().Before(expires) || expires.After(info.ExpiresAt) {
		return ErrTemporaryKeyExpiry
	}
	info.PermanentKeyID = parent
	info.ExpiresAt = expires
	m.temporary[id] = info
	m.expireTemporary(id, expires)
	return nil
}
