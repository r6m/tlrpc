package mtproto

import (
	"bytes"
	"fmt"
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

func TestNestedMessageDecodingSafety(t *testing.T) {
	var key crypto.AuthKey
	for i := range key {
		key[i] = byte(i + 1)
	}

	tests := []struct {
		name   string
		decode func() error
	}{
		{
			name: "unencrypted body length exceeds remaining input",
			decode: func() error {
				buf := &bytes.Buffer{}
				buf.Write(make([]byte, 8))
				if err := WriteInt64(buf, 1); err != nil {
					return err
				}
				if err := WriteInt32(buf, 64); err != nil {
					return err
				}
				buf.WriteByte(0xaa)

				var msg UnencryptedMessage
				if err := msg.Deserialize(buf.Bytes()); err != nil {
					return err
				}
				return nil
			},
		},
		{
			name: "encrypted ciphertext is not block aligned",
			decode: func() error {
				msg := &EncryptedMessage{EncryptedData: make([]byte, 17)}
				_, err := msg.Decrypt(key)
				return err
			},
		},
		{
			name: "encrypted inner data length exceeds remaining plaintext",
			decode: func() error {
				msg, err := encryptedMessageWithDeclaredDataLength(key, 17, 16)
				if err != nil {
					return err
				}
				_, err = msg.Decrypt(key)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			panicValue, err := callDecodeWithoutPanic(tt.decode)
			if panicValue != nil {
				t.Errorf("decoder panicked: %v", panicValue)
				return
			}
			if err == nil {
				t.Error("decoder accepted truncated or malformed input")
			}
		})
	}
}

func encryptedMessageWithDeclaredDataLength(key crypto.AuthKey, declared, remaining int32) (*EncryptedMessage, error) {
	plaintext := &bytes.Buffer{}
	for _, value := range []int64{1, 2, 3} {
		if err := WriteInt64(plaintext, value); err != nil {
			return nil, err
		}
	}
	if err := WriteInt32(plaintext, 1); err != nil {
		return nil, err
	}
	if err := WriteInt32(plaintext, declared); err != nil {
		return nil, err
	}
	if remaining < 0 {
		return nil, fmt.Errorf("negative remaining plaintext: %d", remaining)
	}
	plaintext.Write(make([]byte, remaining))
	if plaintext.Len()%16 != 0 {
		return nil, fmt.Errorf("test plaintext length %d is not block aligned", plaintext.Len())
	}

	msgKey := crypto.ComputeMsgKey(key[:], plaintext.Bytes(), false)
	aesKey, aesIV := crypto.ComputeKDF(key[:], msgKey, false)
	block := crypto.NewAESIGE(aesKey, aesIV)
	ciphertext := make([]byte, plaintext.Len())
	block.CryptBlocks(ciphertext, plaintext.Bytes())
	return &EncryptedMessage{
		AuthKeyID:     key.ID(),
		MsgKey:        msgKey,
		EncryptedData: ciphertext,
	}, nil
}

func callDecodeWithoutPanic(decode func() error) (panicValue any, err error) {
	defer func() {
		panicValue = recover()
	}()
	return nil, decode()
}
