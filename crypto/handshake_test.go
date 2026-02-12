package crypto

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

func TestHandshakeProcess(t *testing.T) {
	clientNonce := bytes.Repeat([]byte{0x01}, 32)
	serverNonce := bytes.Repeat([]byte{0x02}, 32)

	h := &Handshake{rng: bytes.NewReader(serverNonce)}
	resp, key, err := h.Process(clientNonce)
	if err != nil {
		t.Fatalf("process error: %v", err)
	}
	if !bytes.Equal(resp, serverNonce) {
		t.Fatalf("response nonce mismatch")
	}

	sum := sha256.Sum256(append(clientNonce, serverNonce...))
	var expected AuthKey
	copy(expected[:], sum[:])
	if !key.Equal(expected) {
		t.Fatalf("derived key mismatch")
	}
}

func TestHandshakeInvalidRequest(t *testing.T) {
	h := &Handshake{rng: bytes.NewReader(nil)}
	if _, _, err := h.Process([]byte{0x01}); err != ErrHandshakeRequest {
		t.Fatalf("expected invalid request error")
	}
}
