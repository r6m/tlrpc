package runtime

import (
	"bytes"
	"errors"
	"testing"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
)

func TestDecodeFrameSeparatesUnencryptedAndEncryptedTraffic(t *testing.T) {
	unencrypted := &mtproto.UnencryptedMessage{MsgID: 4, Data: constructorBody(0x01020304)}
	frame, err := unencrypted.Serialize()
	if err != nil {
		t.Fatalf("serialize unencrypted: %v", err)
	}
	decoded, err := DecodeFrame(frame, nil)
	if err != nil || decoded.Unencrypted == nil || decoded.Encrypted != nil {
		t.Fatalf("decode unencrypted = %+v, %v", decoded, err)
	}

	var authKey crypto.AuthKey
	for index := range authKey {
		authKey[index] = byte(index + 1)
	}
	inner := &mtproto.InnerData{Salt: 7, SessionID: 9, MsgID: 12, SeqNo: 1, Data: constructorBody(0x05060708)}
	encrypted, err := inner.EncryptFromClient(authKey, authKey.ID())
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	encryptedFrame := serializeEncryptedFrame(encrypted)
	decoded, err = DecodeFrame(encryptedFrame, authKeyMap{authKey.ID(): authKey})
	if err != nil {
		t.Fatalf("decode encrypted: %v", err)
	}
	if decoded.Encrypted == nil || decoded.Unencrypted != nil || !bytes.Equal(decoded.Encrypted.Data, inner.Data) {
		t.Fatalf("decoded encrypted frame = %+v", decoded)
	}
}

func TestDecodeFrameRejectsMalformedAndMismatchedKeys(t *testing.T) {
	if _, err := DecodeFrame(make([]byte, 7), nil); !errors.Is(err, ErrFrameTooShort) {
		t.Fatalf("short frame error = %v", err)
	}
	var authKey crypto.AuthKey
	authKey[0] = 1
	inner := &mtproto.InnerData{Salt: 7, SessionID: 9, MsgID: 12, SeqNo: 1, Data: constructorBody(1)}
	encrypted, err := inner.EncryptFromClient(authKey, authKey.ID())
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	frame := serializeEncryptedFrame(encrypted)
	other := authKey
	other[1] = 2
	if _, err := DecodeFrame(frame, authKeyMap{authKey.ID(): other}); !errors.Is(err, ErrAuthKeyMismatch) {
		t.Fatalf("mismatched key error = %v", err)
	}
}

type authKeyMap map[crypto.KeyID]crypto.AuthKey

func (m authKeyMap) Get(keyID crypto.KeyID) (crypto.AuthKey, error) {
	key, ok := m[keyID]
	if !ok {
		return crypto.AuthKey{}, crypto.ErrAuthKeyNotFound
	}
	return key, nil
}
