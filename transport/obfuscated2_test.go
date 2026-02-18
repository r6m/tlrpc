package transport

import (
	"bytes"
	"testing"
)

func TestObfuscatedRoundTrip(t *testing.T) {
	header, clientStreams, err := NewClientObfuscated(0xEEEEEEEE, nil)
	if err != nil {
		t.Fatalf("client init: %v", err)
	}
	serverStreams, err := NewServerObfuscated(header, nil)
	if err != nil {
		t.Fatalf("server init: %v", err)
	}
	if serverStreams.Tag != 0xEEEEEEEE {
		t.Fatalf("tag mismatch: %x", serverStreams.Tag)
	}

	plaintext := bytes.Repeat([]byte{0x7f}, 64)
	enc := make([]byte, len(plaintext))
	copy(enc, plaintext)
	clientStreams.Encrypt.XORKeyStream(enc, enc)
	dec := make([]byte, len(enc))
	copy(dec, enc)
	serverStreams.Decrypt.XORKeyStream(dec, dec)
	if !bytes.Equal(dec, plaintext) {
		t.Fatalf("decrypt mismatch")
	}

	resp := bytes.Repeat([]byte{0x55}, 32)
	encResp := make([]byte, len(resp))
	copy(encResp, resp)
	serverStreams.Encrypt.XORKeyStream(encResp, encResp)
	decResp := make([]byte, len(encResp))
	copy(decResp, encResp)
	clientStreams.Decrypt.XORKeyStream(decResp, decResp)
	if !bytes.Equal(decResp, resp) {
		t.Fatalf("response decrypt mismatch")
	}
}

func TestObfuscatedInvalidHeader(t *testing.T) {
	_, err := NewServerObfuscated(make([]byte, 64), nil)
	if err == nil {
		t.Fatalf("expected error for invalid header")
	}
}
