package crypto

import (
	"crypto/sha1"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"time"
)

const MaxTemporaryAuthKeyLifetime = 24 * time.Hour

var (
	ErrTemporaryKeyUnsupported  = errors.New("crypto: temporary auth keys unsupported")
	ErrTemporaryKeyAlreadyBound = errors.New("crypto: temporary auth key already bound")
	ErrTemporaryKeyExpiry       = errors.New("crypto: invalid temporary auth key expiry")
	ErrInvalidKeyBinding        = errors.New("crypto: invalid temporary auth key binding")
)

type TemporaryAuthKeyInfo struct {
	ExpiresAt      time.Time
	PermanentKeyID KeyID
}

// TemporaryAuthKeyManager stores temporary secrets only in volatile memory.
// Metadata may be durable. Missing/expired/revoked keys return ErrAuthKeyNotFound.
// BindTemporary must atomically reject rebinding to a different permanent key.
type TemporaryAuthKeyManager interface {
	AuthKeyManager
	PutTemporary(KeyID, AuthKey, time.Time) error
	TemporaryInfo(KeyID) (TemporaryAuthKeyInfo, bool, error)
	BindTemporary(temp, permanent KeyID, expiresAt time.Time) error
}

// ValidateTemporaryKeyBinding verifies the MTProto 1.0 envelope used only for
// auth.bindTempAuthKey. Ordinary transport traffic continues to use MTProto 2.0.
func ValidateTemporaryKeyBinding(permanent AuthKey, temp KeyID, sessionID, messageID, nonce int64, expiresAt int32, encrypted []byte) error {
	// The inner payload is exactly 40 bytes, plus a 32-byte envelope and
	// 8-byte alignment padding. No attacker-controlled allocations or lengths.
	if len(encrypted) != 24+80 || KeyID(binary.LittleEndian.Uint64(encrypted)) != permanent.ID() {
		return ErrInvalidKeyBinding
	}
	msgKey := encrypted[8:24]
	key, iv := temporaryBindingKDF(permanent[:], msgKey)
	plain := make([]byte, 80)
	NewAESIGEDecrypt(key, iv).CryptBlocks(plain, encrypted[24:])
	hash := sha1.Sum(plain[:72])
	if subtle.ConstantTimeCompare(hash[4:20], msgKey) != 1 {
		return ErrInvalidKeyBinding
	}
	if int64(binary.LittleEndian.Uint64(plain[16:])) != messageID || binary.LittleEndian.Uint32(plain[24:]) != 0 || binary.LittleEndian.Uint32(plain[28:]) != 40 || binary.LittleEndian.Uint32(plain[32:]) != 0x75a3f765 {
		return ErrInvalidKeyBinding
	}
	if int64(binary.LittleEndian.Uint64(plain[36:])) != nonce || KeyID(binary.LittleEndian.Uint64(plain[44:])) != temp || KeyID(binary.LittleEndian.Uint64(plain[52:])) != permanent.ID() || int64(binary.LittleEndian.Uint64(plain[60:])) != sessionID || int32(binary.LittleEndian.Uint32(plain[68:])) != expiresAt {
		return ErrInvalidKeyBinding
	}
	return nil
}

func temporaryBindingKDF(authKey, msgKey []byte) ([]byte, []byte) {
	hash := func(parts ...[]byte) []byte {
		h := sha1.New()
		for _, p := range parts {
			h.Write(p)
		}
		return h.Sum(nil)
	}
	a := hash(msgKey, authKey[:32])
	b := hash(authKey[32:48], msgKey, authKey[48:64])
	c := hash(authKey[64:96], msgKey)
	d := hash(msgKey, authKey[96:128])
	key := append(append(append([]byte{}, a[:8]...), b[8:20]...), c[4:16]...)
	iv := append(append(append(append([]byte{}, a[8:20]...), b[:8]...), c[16:20]...), d[:8]...)
	return key, iv
}
