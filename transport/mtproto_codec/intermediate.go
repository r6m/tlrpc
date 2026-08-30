package mtprotocodec

import (
	"encoding/binary"
	"io"
)

const intermediateTag = 0xEEEEEEEE

type Intermediate struct {
	AllowQuickAckTokens bool
}

func (i *Intermediate) ProtocolTag() uint32 { return intermediateTag }

func (i *Intermediate) ReadPacket(r io.Reader, maxPayloadBytes int) ([]byte, *uint32, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, nil, err
	}
	length := binary.LittleEndian.Uint32(header[:])
	if length&0x80000000 != 0 {
		token := length & 0x7fffffff
		if i.AllowQuickAckTokens {
			return nil, &token, ErrQuickAck
		}
	}
	payloadLen, err := checkedPayloadLength(uint64(length), maxPayloadBytes)
	if err != nil {
		return nil, nil, err
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, nil, err
	}
	if err := readTransportError(payload); err != nil {
		return nil, nil, err
	}
	return payload, nil, nil
}

func (i *Intermediate) WritePacket(w io.Writer, payload []byte) error {
	length := uint32(len(payload))
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], length)
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}
