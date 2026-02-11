// Package crypto provides AES-256-IGE encryption for MTProto.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
)

// AESIGE implements AES-256-IGE encryption/decryption.
type AESIGE struct {
	cipher cipher.Block
}

// NewAESIGE creates a new AES-IGE cipher.
func NewAESIGE(key []byte) (*AESIGE, error) {
	if len(key) != 32 {
		return nil, errors.New("key must be 32 bytes")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	return &AESIGE{cipher: block}, nil
}

// Encrypt encrypts plaintext using AES-256-IGE.
func (a *AESIGE) Encrypt(plaintext, iv []byte) ([]byte, error) {
	if len(iv) != 32 {
		return nil, errors.New("IV must be 32 bytes")
	}

	ciphertext := make([]byte, len(plaintext))
	mode := cipher.NewCBCEncrypter(a.cipher, iv[:16])
	mode.CryptBlocks(ciphertext, plaintext)

	// TODO: Implement proper IGE mode
	// This is a simplified CBC implementation for now

	return ciphertext, nil
}

// Decrypt decrypts ciphertext using AES-256-IGE.
func (a *AESIGE) Decrypt(ciphertext, iv []byte) ([]byte, error) {
	if len(iv) != 32 {
		return nil, errors.New("IV must be 32 bytes")
	}

	plaintext := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(a.cipher, iv[:16])
	mode.CryptBlocks(plaintext, ciphertext)

	// TODO: Implement proper IGE mode
	// This is a simplified CBC implementation for now

	return plaintext, nil
}

// DeriveKey derives a session key from auth key and message key.
func DeriveKey(authKey []byte, msgKey []byte, isOutgoing bool) ([]byte, []byte, error) {
	// Simplified key derivation for now
	// Real MTProto uses more complex derivation
	key := make([]byte, 32)
	copy(key, authKey[:32])

	iv := make([]byte, 32)
	copy(iv, msgKey)

	return key, iv, nil
}