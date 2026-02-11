// Package crypto provides MTProto cryptographic primitives.
package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
)

// AuthKey represents an MTProto authorization key.
type AuthKey struct {
	ID     int64
	Key    []byte
	Sha256 []byte
}

// NewAuthKey creates a new authorization key.
func NewAuthKey(key []byte) (*AuthKey, error) {
	if len(key) != 256 {
		return nil, errors.New("auth key must be 256 bytes")
	}

	sha := sha256.Sum256(key)
	id := int64(sha[0]) | int64(sha[1])<<8 | int64(sha[2])<<16 | int64(sha[3])<<24 |
		int64(sha[4])<<32 | int64(sha[5])<<40 | int64(sha[6])<<48 | int64(sha[7])<<56

	return &AuthKey{
		ID:     id,
		Key:    key,
		Sha256: sha[:],
	}, nil
}

// GenerateAuthKey generates a new random authorization key.
func GenerateAuthKey() (*AuthKey, error) {
	key := make([]byte, 256)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return NewAuthKey(key)
}

// TempAuthKey represents a temporary authorization key for PFS.
type TempAuthKey struct {
	*AuthKey
	Expires int64
}

// NewTempAuthKey creates a new temporary authorization key.
func NewTempAuthKey(key []byte, expires int64) (*TempAuthKey, error) {
	authKey, err := NewAuthKey(key)
	if err != nil {
		return nil, err
	}

	return &TempAuthKey{
		AuthKey: authKey,
		Expires: expires,
	}, nil
}

// IsExpired checks if the temporary key has expired.
func (k *TempAuthKey) IsExpired(now int64) bool {
	return k.Expires < now
}