package mtprotocodec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

var (
	ErrInvalidLength = errors.New("transport: invalid packet length")
	ErrQuickAck      = errors.New("transport: quick ack")
)

type TransportError struct {
	Code int32
}

func (e TransportError) Error() string {
	return fmt.Sprintf("transport: error code %d", e.Code)
}

type Codec interface {
	ReadPacket(r io.Reader) (payload []byte, quickAck *uint32, err error)
	WritePacket(w io.Writer, payload []byte) error
	ProtocolTag() uint32
}

func readTransportError(payload []byte) error {
	if len(payload) != 4 {
		return nil
	}
	code := int32(binary.LittleEndian.Uint32(payload))
	if code >= 0 {
		return nil
	}
	return TransportError{Code: code}
}

func writeFullPacket(w io.Writer, seqno uint32, payload []byte) error {
	length := uint32(len(payload) + 12)
	var header [8]byte
	binary.LittleEndian.PutUint32(header[:4], length)
	binary.LittleEndian.PutUint32(header[4:], seqno)
	crc := crc32.NewIEEE()
	if _, err := crc.Write(header[:]); err != nil {
		return err
	}
	if _, err := crc.Write(payload); err != nil {
		return err
	}
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	var crcBuf [4]byte
	binary.LittleEndian.PutUint32(crcBuf[:], crc.Sum32())
	_, err := w.Write(crcBuf[:])
	return err
}
