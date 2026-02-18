# Best Practices

- Keep schema and generated code versioned together.
- Register all services at startup, fail fast on registration errors.
- Use `WithUnaryInterceptor` for auth/logging/recovery/metrics.
- Return explicit MTProto error messages (`PHONE_NUMBER_INVALID`, etc.).
- Keep handler logic stateless where possible; persist session/business state explicitly.
- Validate every request field before downstream calls.
- Set up idempotency/dedupe based on session + message IDs for side-effecting operations.
- Load test with container (`msg_container`) traffic patterns, not only single RPC calls.

## Storage With GORM (Example)

Below are minimal examples showing how to persist sessions and auth keys with GORM. These implement the `session.Manager` and `crypto.AuthKeyManager` interfaces and can be wired into the server with `WithSessionManager` and `WithAuthKeyManager`.

### Session Manager (GORM)

```go
package storage

import (
	"errors"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/session"
	"gorm.io/gorm"
)

type SessionRecord struct {
	ID           int64 `gorm:"primaryKey"`
	AuthKeyID    uint64 `gorm:"uniqueIndex"`
	Layer        int
	UserID       int64
	ServerSalt   int64
	SessionID    int64
	LastMsgID    int64
	SeqNo        int32
	CreatedAt    time.Time
	LastActivity time.Time
}

// GormSessionManager implements session.Manager.
type GormSessionManager struct {
	db *gorm.DB
}

func NewGormSessionManager(db *gorm.DB) *GormSessionManager {
	return &GormSessionManager{db: db}
}

func (m *GormSessionManager) Get(authKeyID crypto.KeyID) (*session.Session, error) {
	var rec SessionRecord
	if err := m.db.Where("auth_key_id = ?", uint64(authKeyID)).First(&rec).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, session.ErrSessionNotFound
		}
		return nil, err
	}
	return &session.Session{
		ID:              rec.ID,
		AuthKeyID:       crypto.KeyID(rec.AuthKeyID),
		Layer:           rec.Layer,
		UserID:          rec.UserID,
		ServerSalt:      rec.ServerSalt,
		SessionID:       rec.SessionID,
		LastClientMsgID: rec.LastMsgID,
		SeqNo:           rec.SeqNo,
		CreatedAt:       rec.CreatedAt,
		LastActivity:    rec.LastActivity,
	}, nil
}

func (m *GormSessionManager) Create(authKeyID crypto.KeyID) (*session.Session, error) {
	sess := &session.Session{
		ID:           time.Now().UnixNano(),
		AuthKeyID:    authKeyID,
		ServerSalt:   time.Now().UTC().UnixNano(),
		CreatedAt:    time.Now().UTC(),
		LastActivity: time.Now().UTC(),
	}
	if err := m.Save(sess); err != nil {
		return nil, err
	}
	return sess, nil
}

func (m *GormSessionManager) Save(sess *session.Session) error {
	if sess == nil {
		return nil
	}
	rec := SessionRecord{
		ID:           sess.ID,
		AuthKeyID:    uint64(sess.AuthKeyID),
		Layer:        sess.Layer,
		UserID:       sess.UserID,
		ServerSalt:   sess.ServerSalt,
		SessionID:    sess.SessionID,
		LastMsgID:    sess.LastClientMsgID,
		SeqNo:        sess.SeqNo,
		CreatedAt:    sess.CreatedAt,
		LastActivity: sess.LastActivity,
	}
	return m.db.Save(&rec).Error
}

func (m *GormSessionManager) Delete(authKeyID crypto.KeyID) error {
	return m.db.Where("auth_key_id = ?", uint64(authKeyID)).Delete(&SessionRecord{}).Error
}

func (m *GormSessionManager) GC(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	_ = m.db.Where("last_activity < ?", cutoff).Delete(&SessionRecord{}).Error
}
```

### Auth Key Manager (GORM)

```go
package storage

import (
	"errors"

	"github.com/r6m/tlrpc/crypto"
	"gorm.io/gorm"
)

type AuthKeyRecord struct {
	KeyID uint64 `gorm:"primaryKey"`
	Key   []byte `gorm:"size:256"`
}

// GormAuthKeyManager implements crypto.AuthKeyManager.
type GormAuthKeyManager struct {
	db *gorm.DB
}

func NewGormAuthKeyManager(db *gorm.DB) *GormAuthKeyManager {
	return &GormAuthKeyManager{db: db}
}

func (m *GormAuthKeyManager) Get(keyID crypto.KeyID) (crypto.AuthKey, error) {
	var rec AuthKeyRecord
	if err := m.db.First(&rec, "key_id = ?", uint64(keyID)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return crypto.AuthKey{}, crypto.ErrAuthKeyNotFound
		}
		return crypto.AuthKey{}, err
	}
	var key crypto.AuthKey
	copy(key[:], rec.Key)
	return key, nil
}

func (m *GormAuthKeyManager) Put(keyID crypto.KeyID, key crypto.AuthKey) error {
	rec := AuthKeyRecord{KeyID: uint64(keyID), Key: key[:]}
	return m.db.Save(&rec).Error
}

func (m *GormAuthKeyManager) Delete(keyID crypto.KeyID) error {
	return m.db.Delete(&AuthKeyRecord{}, "key_id = ?", uint64(keyID)).Error
}
```

### Wiring

```go
srv := tlrpc.NewServer(
	tlrpc.WithSessionManager(storage.NewGormSessionManager(db)),
	tlrpc.WithAuthKeyManager(storage.NewGormAuthKeyManager(db)),
)
```
