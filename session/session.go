package session

import (
	"sync"
	"time"

	"github.com/r6m/tlrpc/crypto"
)

// Session represents a client session.
type Session struct {
	ID           int64
	AuthKeyID    crypto.KeyID
	Layer        int
	UserID       int64
	CreatedAt    time.Time
	LastActivity time.Time

	// Sequence numbers for message ordering.
	SeqNo int32

	// Message IDs for deduplication.
	RecentMsgIDs *LRUCache

	// User-defined storage.
	Data sync.Map
}

// IsAuthorized returns true if the session is authorized.
func (s *Session) IsAuthorized() bool {
	return s.UserID != 0
}

// Touch updates session last activity.
func (s *Session) Touch() {
	s.LastActivity = time.Now().UTC()
}
