package mtprotocodec

import (
	"encoding/binary"
	"io"
)

const paddedIntermediateTag = 0xDDDDDDDD

type PaddedIntermediate struct {
	AllowQuickAckTokens bool
}

func (p *PaddedIntermediate) ProtocolTag() uint32 { return paddedIntermediateTag }

func (p *PaddedIntermediate) ReadPacket(r io.Reader) ([]byte, *uint32, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, nil, err
	}
	length := binary.LittleEndian.Uint32(header[:])
	if length&0x80000000 != 0 {
		token := length & 0x7fffffff
		if p.AllowQuickAckTokens {
			return nil, &token, ErrQuickAck
		}
	}
	if length == 0 || length > 1<<24 {
		return nil, nil, ErrInvalidLength
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, nil, err
	}
	if err := readTransportError(payload); err != nil {
		return nil, nil, err
	}
	return payload, nil, nil
}

func (p *PaddedIntermediate) WritePacket(w io.Writer, payload []byte) error {
	// Keep packet boundaries deterministic at framework level.
	// Stream obfuscation/noise should come from obfuscated2.
	length := uint32(len(payload))
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], length)
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}
