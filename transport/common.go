package transport

import (
	"encoding/binary"
	"errors"
	"io"
)

var ErrInvalidLength = errors.New("transport: invalid frame length")

func readFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	length := binary.LittleEndian.Uint32(header[:])
	if length < 4 {
		return nil, ErrInvalidLength
	}
	payloadLen := int(length - 4)
	if payloadLen < 0 {
		return nil, ErrInvalidLength
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	padding := (4 - (payloadLen % 4)) % 4
	if padding > 0 {
		var pad [3]byte
		if _, err := io.ReadFull(r, pad[:padding]); err != nil {
			return nil, err
		}
	}
	return payload, nil
}

func writeFrame(w io.Writer, payload []byte) error {
	length := uint32(len(payload) + 4)
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], length)
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	padding := (4 - (len(payload) % 4)) % 4
	if padding > 0 {
		_, err := w.Write(make([]byte, padding))
		return err
	}
	return nil
}
