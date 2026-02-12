package crypto

import (
	"crypto/sha1"
	"crypto/subtle"
	"encoding/binary"
	"errors"
)

// AuthKey is a 256-bit shared secret.
type AuthKey [32]byte

// KeyID is the first 64 bits of SHA1(auth_key).
type KeyID uint64

var ErrAuthKeyNotFound = errors.New("crypto: auth key not found")

// ID returns the KeyID for the auth key.
func (k AuthKey) ID() KeyID {
	hash := sha1.Sum(k[:])
	return KeyID(binary.LittleEndian.Uint64(hash[:8]))
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
