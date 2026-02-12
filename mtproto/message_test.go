package mtproto

import (
	"bytes"
	"testing"

	"github.com/r6m/tlrpc/crypto"
)

func TestUnencryptedMessageSerialize(t *testing.T) {
	msg := &UnencryptedMessage{
		MsgID: 12345,
		Data:  []byte("payload"),
	}
	encoded, err := msg.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	var decoded UnencryptedMessage
	if err := decoded.Deserialize(encoded); err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	if decoded.MsgID != msg.MsgID {
		t.Fatalf("msg id mismatch")
	}
	if !bytes.Equal(decoded.Data, msg.Data) {
		t.Fatalf("data mismatch")
	}
}

func TestEncryptedMessageRoundTrip(t *testing.T) {
	var key crypto.AuthKey
	for i := 0; i < len(key); i++ {
		key[i] = byte(i + 1)
	}

	inner := &InnerData{
		Salt:      1,
		SessionID: 2,
		MsgID:     3,
		SeqNo:     4,
		Data:      []byte("hello"),
	}

	enc, err := inner.Encrypt(key, key.ID())
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	dec, err := enc.Decrypt(key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if dec.Salt != inner.Salt || dec.SessionID != inner.SessionID || dec.MsgID != inner.MsgID || dec.SeqNo != inner.SeqNo {
		t.Fatalf("inner header mismatch")
	}
	if !bytes.Equal(dec.Data, inner.Data) {
		t.Fatalf("inner data mismatch")
	}
}

func TestMsgKeyMismatch(t *testing.T) {
	var key crypto.AuthKey
	for i := 0; i < len(key); i++ {
		key[i] = byte(0xAA)
	}
	inner := &InnerData{Salt: 1, SessionID: 2, MsgID: 3, SeqNo: 4, Data: []byte("data")}
	enc, err := inner.Encrypt(key, key.ID())
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	enc.MsgKey[0] ^= 0xFF
	if _, err := enc.Decrypt(key); err != ErrMsgKeyMismatch {
		t.Fatalf("expected msg_key mismatch")
	}
}
