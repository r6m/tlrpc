package mtproto

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"io"

	"github.com/r6m/tlrpc/crypto"
)

var (
	ErrMsgKeyMismatch       = errors.New("mtproto: msg_key mismatch")
	ErrInvalidMessageLength = errors.New("mtproto: invalid message length")
)

// UnencryptedMessage is used for handshake only.
type UnencryptedMessage struct {
	AuthKeyID [8]byte
	MsgID     int64
	Data      []byte
}

// EncryptedMessage is used for all RPC calls.
type EncryptedMessage struct {
	AuthKeyID     crypto.KeyID
	MsgKey        [16]byte
	EncryptedData []byte
}

// InnerData is the decrypted payload.
type InnerData struct {
	Salt      int64
	SessionID int64
	MsgID     int64
	SeqNo     int32
	Data      []byte
}

// Serialize encodes an unencrypted message.
func (m *UnencryptedMessage) Serialize() ([]byte, error) {
	buf := &bytes.Buffer{}
	if _, err := buf.Write(m.AuthKeyID[:]); err != nil {
		return nil, err
	}
	if err := WriteInt64(buf, m.MsgID); err != nil {
		return nil, err
	}
	if err := WriteInt32(buf, int32(len(m.Data))); err != nil {
		return nil, err
	}
	if _, err := buf.Write(m.Data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Deserialize decodes an unencrypted message.
func (m *UnencryptedMessage) Deserialize(data []byte) error {
	reader := bytes.NewReader(data)
	if _, err := io.ReadFull(reader, m.AuthKeyID[:]); err != nil {
		return err
	}
	msgID, err := ReadInt64(reader)
	if err != nil {
		return err
	}
	length, err := ReadInt32(reader)
	if err != nil {
		return err
	}
	if length < 0 {
		return ErrInvalidMessageLength
	}
	msgData := make([]byte, length)
	if _, err := io.ReadFull(reader, msgData); err != nil {
		return err
	}
	m.MsgID = msgID
	m.Data = msgData
	return nil
}

// Decrypt decrypts the message payload into inner data.
func (m *EncryptedMessage) Decrypt(key crypto.AuthKey) (*InnerData, error) {
	aesKey, aesIV := deriveAESKeyIV(key, m.MsgKey)
	block := crypto.NewAESIGEDecrypt(aesKey, aesIV)
	plaintext := make([]byte, len(m.EncryptedData))
	block.CryptBlocks(plaintext, m.EncryptedData)

	msgKey := calcMsgKey(key, plaintext)
	if !bytes.Equal(msgKey[:], m.MsgKey[:]) {
		return nil, ErrMsgKeyMismatch
	}

	inner := &InnerData{}
	reader := bytes.NewReader(plaintext)
	var err error
	if inner.Salt, err = ReadInt64(reader); err != nil {
		return nil, err
	}
	if inner.SessionID, err = ReadInt64(reader); err != nil {
		return nil, err
	}
	if inner.MsgID, err = ReadInt64(reader); err != nil {
		return nil, err
	}
	if inner.SeqNo, err = ReadInt32(reader); err != nil {
		return nil, err
	}
	dataLen, err := ReadInt32(reader)
	if err != nil {
		return nil, err
	}
	if dataLen < 0 {
		return nil, ErrInvalidMessageLength
	}
	inner.Data = make([]byte, dataLen)
	if _, err := io.ReadFull(reader, inner.Data); err != nil {
		return nil, err
	}
	return inner, nil
}

// Encrypt encrypts inner data into an encrypted message.
func (m *InnerData) Encrypt(key crypto.AuthKey, authKeyID crypto.KeyID) (*EncryptedMessage, error) {
	plaintext, err := m.serialize()
	if err != nil {
		return nil, err
	}
	msgKey := calcMsgKey(key, plaintext)
	aesKey, aesIV := deriveAESKeyIV(key, msgKey)
	block := crypto.NewAESIGE(aesKey, aesIV)
	ciphertext := make([]byte, len(plaintext))
	block.CryptBlocks(ciphertext, plaintext)
	return &EncryptedMessage{
		AuthKeyID:     authKeyID,
		MsgKey:        msgKey,
		EncryptedData: ciphertext,
	}, nil
}

func (m *InnerData) serialize() ([]byte, error) {
	buf := &bytes.Buffer{}
	if err := WriteInt64(buf, m.Salt); err != nil {
		return nil, err
	}
	if err := WriteInt64(buf, m.SessionID); err != nil {
		return nil, err
	}
	if err := WriteInt64(buf, m.MsgID); err != nil {
		return nil, err
	}
	if err := WriteInt32(buf, m.SeqNo); err != nil {
		return nil, err
	}
	if err := WriteInt32(buf, int32(len(m.Data))); err != nil {
		return nil, err
	}
	if _, err := buf.Write(m.Data); err != nil {
		return nil, err
	}
	padding := (16 - (buf.Len() % 16)) % 16
	if padding > 0 {
		pad := make([]byte, padding)
		if _, err := rand.Read(pad); err != nil {
			return nil, err
		}
		if _, err := buf.Write(pad); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func calcMsgKey(key crypto.AuthKey, data []byte) [16]byte {
	var msgKey [16]byte
	h := sha1.New()
	_, _ = h.Write(key[:])
	_, _ = h.Write(data)
	copy(msgKey[:], h.Sum(nil)[:16])
	return msgKey
}

func deriveAESKeyIV(key crypto.AuthKey, msgKey [16]byte) ([]byte, []byte) {
	hashKey := sha256.Sum256(append(msgKey[:], key[:]...))
	hashIV := sha256.Sum256(append(key[:], msgKey[:]...))
	aesKey := make([]byte, 32)
	aesIV := make([]byte, 32)
	copy(aesKey, hashKey[:])
	copy(aesIV, hashIV[:])
	return aesKey, aesIV
}
