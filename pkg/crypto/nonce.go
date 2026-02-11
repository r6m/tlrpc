// Package crypto provides nonce generation utilities.
package crypto

import (
	"crypto/rand"
	"errors"
)

// Nonce represents a cryptographic nonce.
type Nonce [32]byte

// NewNonce generates a new random nonce.
func NewNonce() (Nonce, error) {
	var nonce Nonce
	if _, err := rand.Read(nonce[:]); err != nil {
		return nonce, err
	}
	return nonce, nil
}

// Bytes returns the nonce as bytes.
func (n Nonce) Bytes() []byte {
	return n[:]
}

// String returns the nonce as a hex string.
func (n Nonce) String() string {
	return string(n.Bytes())
}

// NewMessageNonce generates a nonce for message encryption.
func NewMessageNonce() (Nonce, error) {
	return NewNonce()
}

// NewServerNonce generates a nonce for server responses.
func NewServerNonce() (Nonce, error) {
	return NewNonce()
}

// NewNewNonce generates a nonce for key exchange.
func NewNewNonce() (Nonce, error) {
	return NewNonce()
}

// ValidateNonce checks if a nonce has the correct format.
func ValidateNonce(nonce []byte) error {
	if len(nonce) != 32 {
		return errors.New("nonce must be 32 bytes")
	}
	return nil
}