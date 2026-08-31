package crypto

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
)

var (
	ErrInvalidKeyFormat     = errors.New("crypto: invalid RSA key format")
	ErrKeyNotFound          = errors.New("crypto: RSA key not found")
	ErrUnsafeKeyPermissions = errors.New("crypto: private key file has unsafe permissions")
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
	return PublicKeyFingerprint(&key.PublicKey)
}

// PublicKeyFingerprint returns Telegram's canonical RSA public-key
// fingerprint: SHA-1 over TL-serialized modulus and exponent, interpreted from
// the digest's final eight bytes in little-endian order.
func PublicKeyFingerprint(key *rsa.PublicKey) int64 {
	if key == nil || key.N == nil {
		return 0
	}
	var encoded bytes.Buffer
	writeFingerprintTLBytes(&encoded, key.N.Bytes())
	writeFingerprintTLBytes(&encoded, big.NewInt(int64(key.E)).Bytes())
	sum := sha1.Sum(encoded.Bytes())
	return int64(binary.LittleEndian.Uint64(sum[12:20]))
}

func writeFingerprintTLBytes(dst *bytes.Buffer, value []byte) {
	fieldLength := 1
	if len(value) < 254 {
		_ = dst.WriteByte(byte(len(value)))
	} else {
		fieldLength = 4
		_ = dst.WriteByte(254)
		_ = dst.WriteByte(byte(len(value)))
		_ = dst.WriteByte(byte(len(value) >> 8))
		_ = dst.WriteByte(byte(len(value) >> 16))
	}
	_, _ = dst.Write(value)
	for padding := (4 - (fieldLength+len(value))%4) % 4; padding > 0; padding-- {
		_ = dst.WriteByte(0)
	}
}

func LoadPEMPrivateKey(path string) (*ServerKey, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat key file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("%w: mode %04o", ErrUnsafeKeyPermissions, info.Mode().Perm())
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, ErrInvalidKeyFormat
	}

	key, err := parseRSAPrivateKey(block.Bytes)
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

func parseRSAPrivateKey(data []byte) (*rsa.PrivateKey, error) {
	key, pkcs1Err := x509.ParsePKCS1PrivateKey(data)
	if pkcs1Err == nil {
		return key, nil
	}

	parsed, pkcs8Err := x509.ParsePKCS8PrivateKey(data)
	if pkcs8Err != nil {
		return nil, fmt.Errorf("%w: PKCS#1: %v; PKCS#8: %v", ErrInvalidKeyFormat, pkcs1Err, pkcs8Err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%w: PKCS#8 key type %T is not RSA", ErrInvalidKeyFormat, parsed)
	}
	return key, nil
}

func SavePEMPrivateKey(path string, key *ServerKey) (returnErr error) {
	data := x509.MarshalPKCS1PrivateKey(key.Key)
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: data,
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("open key file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); returnErr == nil && closeErr != nil {
			returnErr = fmt.Errorf("close key file: %w", closeErr)
		}
	}()
	if err := file.Chmod(0600); err != nil {
		return fmt.Errorf("secure key file permissions: %w", err)
	}
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("truncate key file: %w", err)
	}
	if _, err := file.Write(pem.EncodeToMemory(block)); err != nil {
		return fmt.Errorf("write key file: %w", err)
	}
	return nil
}

func rsaDecrypt(key *rsa.PrivateKey, data []byte) ([]byte, error) {
	return rsa.DecryptPKCS1v15(rand.Reader, key, data)
}

// DecryptRSA decrypts data using RSA private key (public function)
func DecryptRSA(key *rsa.PrivateKey, data []byte) ([]byte, error) {
	if decoded, err := decodeRSAPad(data, key); err == nil {
		return decoded, nil
	}
	return rsaDecrypt(key, data)
}
