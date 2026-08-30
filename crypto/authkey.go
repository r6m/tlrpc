package crypto

import (
	"crypto/subtle"
	"errors"
)

// AuthKey is a 256-bit shared secret.
type AuthKey [256]byte

// KeyID is the last 64 bits of SHA1(auth_key), decoded little-endian as
// required by MTProto.
type KeyID uint64

var ErrAuthKeyNotFound = errors.New("crypto: auth key not found")

// ID returns the KeyID for the auth key.
func (k AuthKey) ID() KeyID {
	return KeyID(ComputeAuthKeyID(k[:]))
}

// Equal compares two auth keys in constant time.
func (k AuthKey) Equal(other AuthKey) bool {
	return subtle.ConstantTimeCompare(k[:], other[:]) == 1
}

// AuthKeyManager stores and retrieves keys.
type AuthKeyManager interface {
	Get(keyID KeyID) (AuthKey, error)
	Put(keyID KeyID, key AuthKey) error
	Delete(keyID KeyID) error
}
