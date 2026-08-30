package session

import (
	"errors"

	"github.com/r6m/tlrpc/crypto"
)

var ErrSessionNotFound = errors.New("session: not found")

var ErrSessionKeyMismatch = errors.New("session: key does not match snapshot")

// SessionKey is the complete durable identity of one protocol session.
// A single authorization key may own multiple independent sessions.
type SessionKey struct {
	AuthKeyID crypto.KeyID
	SessionID int64
}
