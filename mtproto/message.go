package mtproto

import (
	"bytes"
	stdaes "crypto/aes"
	"crypto/rand"
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
	msgData, err := ReadSizedBytes(reader, int(length))
	if err != nil {
		return err
	}
	m.MsgID = msgID
	m.Data = msgData
	return nil
}

func (m *EncryptedMessage) decryptWithDirection(key crypto.AuthKey, fromClient bool) (*InnerData, error) {
	if len(m.EncryptedData) == 0 || len(m.EncryptedData)%stdaes.BlockSize != 0 {
		return nil, ErrInvalidMessageLength
	}
	aesKey, aesIV := crypto.ComputeKDF(key[:], m.MsgKey, fromClient)
	block := crypto.NewAESIGEDecrypt(aesKey, aesIV)
	plaintext := make([]byte, len(m.EncryptedData))
	block.CryptBlocks(plaintext, m.EncryptedData)

	msgKey := calcMsgKey(key, plaintext, fromClient)
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
	inner.Data, err = ReadSizedBytes(reader, int(dataLen))
	if err != nil {
		return nil, err
	}
	return inner, nil
}

// Decrypt decrypts a server-to-client message payload into inner data.
func (m *EncryptedMessage) Decrypt(key crypto.AuthKey) (*InnerData, error) {
	return m.decryptWithDirection(key, false)
}

// DecryptFromClient decrypts a client-to-server message payload into inner data.
func (m *EncryptedMessage) DecryptFromClient(key crypto.AuthKey) (*InnerData, error) {
	return m.decryptWithDirection(key, true)
}

func (m *InnerData) encryptWithDirection(key crypto.AuthKey, authKeyID crypto.KeyID, fromClient bool) (*EncryptedMessage, error) {
	plaintext, err := m.serialize()
	if err != nil {
		return nil, err
	}
	msgKey := calcMsgKey(key, plaintext, fromClient)
	aesKey, aesIV := crypto.ComputeKDF(key[:], msgKey, fromClient)
	block := crypto.NewAESIGE(aesKey, aesIV)
	ciphertext := make([]byte, len(plaintext))
	block.CryptBlocks(ciphertext, plaintext)
	return &EncryptedMessage{
		AuthKeyID:     authKeyID,
		MsgKey:        msgKey,
		EncryptedData: ciphertext,
	}, nil
}

// Encrypt encrypts a server-to-client inner message.
func (m *InnerData) Encrypt(key crypto.AuthKey, authKeyID crypto.KeyID) (*EncryptedMessage, error) {
	return m.encryptWithDirection(key, authKeyID, false)
}

// EncryptFromClient encrypts a client-to-server inner message.
func (m *InnerData) EncryptFromClient(key crypto.AuthKey, authKeyID crypto.KeyID) (*EncryptedMessage, error) {
	return m.encryptWithDirection(key, authKeyID, true)
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
	// MTProto 2.0 padding: 12-1024 bytes, aligned to 16 bytes for AES
	currentLen := buf.Len()
	alignmentPadding := (16 - (currentLen % 16)) % 16

	// Calculate total padding needed (at least 12 bytes, at most 1024 bytes)
	minPadding := 12
	maxPadding := 1024

	// We need at least alignmentPadding + minPadding, but not more than maxPadding
	totalPadding := alignmentPadding
	if totalPadding < minPadding {
		// Add enough to reach minPadding, then align to 16 bytes
		additional := minPadding - totalPadding
		// Round up to next 16-byte boundary
		additional = ((additional + 15) / 16) * 16
		totalPadding += additional
	}

	// Cap at maxPadding
	if totalPadding > maxPadding {
		totalPadding = maxPadding
		// Re-align to 16 bytes
		totalPadding = (totalPadding / 16) * 16
	}

	if totalPadding > 0 {
		pad := make([]byte, totalPadding)
		if _, err := rand.Read(pad); err != nil {
			return nil, err
		}
		if _, err := buf.Write(pad); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func calcMsgKey(key crypto.AuthKey, data []byte, fromClient bool) [16]byte {
	return crypto.ComputeMsgKey(key[:], data, fromClient)
}
