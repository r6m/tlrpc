package compatkeys

import (
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	_ "embed"
	"encoding/binary"
	"encoding/pem"
	"fmt"

	"github.com/r6m/tlrpc/crypto"
)

//go:embed compat_server_key.pem
var compatServerKeyPEM []byte

// ServerKey returns the embedded RSA server key used by compat tooling.
func ServerKey() (*crypto.ServerKey, error) {
	block, _ := pem.Decode(compatServerKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("compatkeys: invalid PEM data")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		parsed, pkcs8Err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if pkcs8Err != nil {
			return nil, fmt.Errorf("compatkeys: parse private key: %w", err)
		}
		var ok bool
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("compatkeys: unsupported private key type %T", parsed)
		}
	}
	sk := &crypto.ServerKey{Key: key}
	sk.ID = generateKeyFingerprint(key)
	sk.Fingerprint = make([]byte, 8)
	binary.LittleEndian.PutUint64(sk.Fingerprint, uint64(sk.ID))
	return sk, nil
}

func generateKeyFingerprint(key *rsa.PrivateKey) int64 {
	nBytes := key.N.Bytes()
	eBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(eBytes, uint32(key.E))

	h := sha1.New()
	_, _ = h.Write(nBytes)
	_, _ = h.Write(eBytes)
	sum := h.Sum(nil)

	return int64(binary.LittleEndian.Uint64(sum[:8]))
}
