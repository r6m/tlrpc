package client

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
)

func serializeTL(fn func(io.Writer) error) ([]byte, error) {
	buf := &bytes.Buffer{}
	if err := fn(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeTL(obj tlrpc.TLObject) ([]byte, error) {
	if obj == nil {
		return nil, errors.New("compat client: nil object")
	}
	buf := &bytes.Buffer{}
	if err := obj.(interface{ SerializeTL(io.Writer) error }).SerializeTL(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeTL(constructors map[uint32]func() tlrpc.TLObject, data []byte) (tlrpc.TLObject, error) {
	if len(data) < 4 {
		return nil, io.ErrUnexpectedEOF
	}
	constructorID := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
	constructor, ok := constructors[constructorID]
	if !ok {
		return nil, tlrpc.NewNotFoundError("UNKNOWN_CONSTRUCTOR")
	}
	obj := constructor()
	reader := bytes.NewReader(data)
	deser, ok := obj.(interface{ DeserializeTL(io.Reader) error })
	if !ok {
		return nil, errors.New("compat client: constructor does not implement DeserializeTL")
	}
	if err := deser.DeserializeTL(reader); err != nil {
		return nil, err
	}
	return obj, nil
}

func serializeEncrypted(msg *mtproto.EncryptedMessage) []byte {
	data := make([]byte, 8+16+len(msg.EncryptedData))
	binary.LittleEndian.PutUint64(data[:8], uint64(msg.AuthKeyID))
	copy(data[8:24], msg.MsgKey[:])
	copy(data[24:], msg.EncryptedData)
	return data
}

func writeUnencrypted(conn connIO, msgID int64, body []byte) error {
	msg := &mtproto.UnencryptedMessage{
		AuthKeyID: [8]byte{},
		MsgID:     msgID,
		Data:      body,
	}
	raw, err := msg.Serialize()
	if err != nil {
		return err
	}
	return conn.WriteMessage(raw)
}

func readUnencrypted(conn connIO) (*mtproto.UnencryptedMessage, error) {
	raw, err := conn.ReadMessage(0)
	if err != nil {
		return nil, err
	}
	msg := &mtproto.UnencryptedMessage{}
	if err := msg.Deserialize(raw); err != nil {
		return nil, err
	}
	return msg, nil
}

type connIO interface {
	ReadMessage(maxPayloadBytes int) ([]byte, error)
	WriteMessage([]byte) error
}

func decryptPacket(packet []byte, key crypto.AuthKey) (*mtproto.InnerData, error) {
	if len(packet) < 24 {
		return nil, io.ErrUnexpectedEOF
	}
	keyID := crypto.KeyID(binary.LittleEndian.Uint64(packet[:8]))
	var msgKey [16]byte
	copy(msgKey[:], packet[8:24])
	dec, err := (&mtproto.EncryptedMessage{
		AuthKeyID:     keyID,
		MsgKey:        msgKey,
		EncryptedData: packet[24:],
	}).Decrypt(key)
	if err != nil {
		return nil, err
	}
	return dec, nil
}
