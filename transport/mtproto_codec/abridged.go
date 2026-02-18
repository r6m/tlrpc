package mtprotocodec

import (
	"encoding/binary"
	"io"
)

const abridgedTag = 0xEF

type Abridged struct {
	AllowQuickAckTokens bool
}

func (a *Abridged) ProtocolTag() uint32 { return 0xEFEFEFEF }

func (a *Abridged) ReadPacket(r io.Reader) ([]byte, *uint32, error) {
	var first [1]byte
	if _, err := io.ReadFull(r, first[:]); err != nil {
		return nil, nil, err
	}
	lengthByte := first[0]
	if a.AllowQuickAckTokens && lengthByte == 0x80 {
		var rest [3]byte
		if _, err := io.ReadFull(r, rest[:]); err != nil {
			return nil, nil, err
		}
		token := binary.LittleEndian.Uint32([]byte{lengthByte, rest[0], rest[1], rest[2]})
		return nil, &token, ErrQuickAck
	}

	var length uint32
	if lengthByte == 0x7f {
		var ext [3]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return nil, nil, err
		}
		length = uint32(ext[0]) | uint32(ext[1])<<8 | uint32(ext[2])<<16
	} else {
		length = uint32(lengthByte & 0x7f)
	}
	if length == 0 {
		return nil, nil, ErrInvalidLength
	}
	payloadLen := int(length) * 4
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, nil, err
	}
	if err := readTransportError(payload); err != nil {
		return nil, nil, err
	}
	return payload, nil, nil
}

func (a *Abridged) WritePacket(w io.Writer, payload []byte) error {
	length := len(payload)
	if length%4 != 0 {
		return ErrInvalidLength
	}
	length /= 4
	if length < 0x7f {
		_, err := w.Write([]byte{byte(length)})
		if err != nil {
			return err
		}
	} else {
		var header [4]byte
		header[0] = 0x7f
		header[1] = byte(length)
		header[2] = byte(length >> 8)
		header[3] = byte(length >> 16)
		if _, err := w.Write(header[:]); err != nil {
			return err
		}
	}
	_, err := w.Write(payload)
	return err
}

func WriteAbridgedHeader(w io.Writer) error {
	_, err := w.Write([]byte{abridgedTag})
	return err
}
