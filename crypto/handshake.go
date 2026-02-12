package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

var ErrHandshakeRequest = errors.New("crypto: invalid handshake request")

// Handshake provides a simplified key exchange for the framework.
// This is not a production MTProto handshake implementation.
type Handshake struct {
	rng io.Reader
}

// Process takes a client nonce and returns a server nonce and derived auth key.
// Request format: 32-byte client nonce.
// Response format: 32-byte server nonce.
func (h *Handshake) Process(req []byte) (resp []byte, key AuthKey, err error) {
	if len(req) != 32 {
		return nil, AuthKey{}, ErrHandshakeRequest
	}
	nonce := make([]byte, 32)
	rng := h.rng
	if rng == nil {
		rng = rand.Reader
	}
	if _, err = io.ReadFull(rng, nonce); err != nil {
		return nil, AuthKey{}, err
	}

	hash := sha256.Sum256(append(req, nonce...))
	copy(key[:], hash[:])
	return nonce, key, nil
}
