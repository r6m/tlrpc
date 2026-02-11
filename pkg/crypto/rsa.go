// Package crypto provides RSA operations for MTProto handshake.
package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
)

// RSAKey represents an RSA key for MTProto.
type RSAKey struct {
	key *rsa.PrivateKey
}

// NewRSAKeyFromPEM creates an RSA key from PEM data.
func NewRSAKeyFromPEM(pemData []byte) (*RSAKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, errors.New("invalid PEM data")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	return &RSAKey{key: key}, nil
}

// EncryptOAEP encrypts data using RSA-OAEP.
func (k *RSAKey) EncryptOAEP(data []byte) ([]byte, error) {
	return rsa.EncryptOAEP(sha256.New(), rand.Reader, &k.key.PublicKey, data, nil)
}

// DecryptOAEP decrypts data using RSA-OAEP.
func (k *RSAKey) DecryptOAEP(data []byte) ([]byte, error) {
	return rsa.DecryptOAEP(sha256.New(), rand.Reader, k.key, data, nil)
}

// PublicKey returns the public key in PKCS1 format.
func (k *RSAKey) PublicKey() []byte {
	return x509.MarshalPKCS1PublicKey(&k.key.PublicKey)
}

// Fingerprint returns the key fingerprint (SHA256 of public key).
func (k *RSAKey) Fingerprint() []byte {
	pubKey := k.PublicKey()
	hash := sha256.Sum256(pubKey)
	return hash[:]
}

// Sign signs data using PSS.
func (k *RSAKey) Sign(data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)
	return rsa.SignPSS(rand.Reader, k.key, 0, hash[:], nil)
}

// Verify verifies a PSS signature.
func (k *RSAKey) Verify(data, signature []byte) error {
	hash := sha256.Sum256(data)
	return rsa.VerifyPSS(&k.key.PublicKey, 0, hash[:], signature, nil)
}