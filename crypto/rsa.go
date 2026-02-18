package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

var (
	ErrInvalidKeyFormat = errors.New("crypto: invalid RSA key format")
	ErrKeyNotFound      = errors.New("crypto: RSA key not found")
)

type ServerKey struct {
	ID          int64
	Key         *rsa.PrivateKey
	Fingerprint []byte
}

type ServerKeyManager interface {
	GetKey(fingerprint []byte) (*ServerKey, error)
	GetAllKeys() ([]*ServerKey, error)
}

type MemoryServerKeyManager struct {
	keys map[int64]*ServerKey
}

func NewMemoryServerKeyManager() *MemoryServerKeyManager {
	return &MemoryServerKeyManager{keys: make(map[int64]*ServerKey)}
}

func (m *MemoryServerKeyManager) AddKey(key *ServerKey) {
	m.keys[key.ID] = key
}

func (m *MemoryServerKeyManager) GetKey(fingerprint []byte) (*ServerKey, error) {
	for _, k := range m.keys {
		if len(k.Fingerprint) == len(fingerprint) {
			match := true
			for i := range fingerprint {
				if k.Fingerprint[i] != fingerprint[i] {
					match = false
					break
				}
			}
			if match {
				return k, nil
			}
		}
	}
	return nil, ErrKeyNotFound
}

func (m *MemoryServerKeyManager) GetAllKeys() ([]*ServerKey, error) {
	keys := make([]*ServerKey, 0, len(m.keys))
	for _, k := range m.keys {
		keys = append(keys, k)
	}
	return keys, nil
}

func GenerateServerKey() (*ServerKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate RSA key: %w", err)
	}
	sk := &ServerKey{
		Key: key,
	}
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
	h.Write(nBytes)
	h.Write(eBytes)
	sum := h.Sum(nil)

	return int64(binary.LittleEndian.Uint64(sum[:8]))
}

func LoadPEMPrivateKey(path string) (*ServerKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, ErrInvalidKeyFormat
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	sk := &ServerKey{
		Key: key,
	}
	sk.ID = generateKeyFingerprint(key)
	sk.Fingerprint = make([]byte, 8)
	binary.LittleEndian.PutUint64(sk.Fingerprint, uint64(sk.ID))
	return sk, nil
}

func SavePEMPrivateKey(path string, key *ServerKey) error {
	data := x509.MarshalPKCS1PrivateKey(key.Key)
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: data,
	}
	return os.WriteFile(path, pem.EncodeToMemory(block), 0600)
}

func rsaDecrypt(key *rsa.PrivateKey, data []byte) ([]byte, error) {
	return rsa.DecryptPKCS1v15(rand.Reader, key, data)
}

// DecryptRSA decrypts data using RSA private key (public function)
func DecryptRSA(key *rsa.PrivateKey, data []byte) ([]byte, error) {
	return rsaDecrypt(key, data)
}
