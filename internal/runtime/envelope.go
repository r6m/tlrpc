package runtime

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
)

var (
	ErrFrameTooShort   = errors.New("runtime: MTProto frame is too short")
	ErrAuthKeyMismatch = errors.New("runtime: auth key ID does not match stored key")
)

type AuthKeySource interface {
	Get(keyID crypto.KeyID) (crypto.AuthKey, error)
}

type DecodedFrame struct {
	Unencrypted *mtproto.UnencryptedMessage
	Encrypted   *mtproto.InnerData
	AuthKeyID   crypto.KeyID
	AuthKey     crypto.AuthKey
}

// DecodeFrame parses one complete bounded transport payload. Exactly one of
// Unencrypted or Encrypted is set on success.
func DecodeFrame(frame []byte, authKeys AuthKeySource) (DecodedFrame, error) {
	if len(frame) < 8 {
		return DecodedFrame{}, ErrFrameTooShort
	}
	keyID := crypto.KeyID(binary.LittleEndian.Uint64(frame[:8]))
	if keyID == 0 {
		message := &mtproto.UnencryptedMessage{}
		if err := message.Deserialize(frame); err != nil {
			return DecodedFrame{}, err
		}
		return DecodedFrame{Unencrypted: message}, nil
	}
	if len(frame) < 24 {
		return DecodedFrame{}, ErrFrameTooShort
	}
	if authKeys == nil {
		return DecodedFrame{}, errors.New("runtime: auth key source is required")
	}
	authKey, err := authKeys.Get(keyID)
	if err != nil {
		return DecodedFrame{}, fmt.Errorf("runtime: load auth key %d: %w", keyID, err)
	}
	if authKey.ID() != keyID {
		return DecodedFrame{}, ErrAuthKeyMismatch
	}
	message := &mtproto.EncryptedMessage{AuthKeyID: keyID, EncryptedData: append([]byte(nil), frame[24:]...)}
	copy(message.MsgKey[:], frame[8:24])
	inner, err := message.DecryptFromClient(authKey)
	if err != nil {
		return DecodedFrame{}, err
	}
	return DecodedFrame{Encrypted: inner, AuthKeyID: keyID, AuthKey: authKey}, nil
}

func EncodeUnencryptedFrame(message *mtproto.UnencryptedMessage) ([]byte, error) {
	if message == nil {
		return nil, io.ErrUnexpectedEOF
	}
	return message.Serialize()
}
