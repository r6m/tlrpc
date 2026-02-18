package mtprotocodec

import (
	"encoding/binary"
	"hash/crc32"
	"io"
	"sync"
)

const fullMinLen = 12

type Full struct {
	mu   sync.Mutex
	next uint32
}

func (f *Full) ProtocolTag() uint32 { return 0 }

func (f *Full) ReadPacket(r io.Reader) ([]byte, *uint32, error) {
	var header [8]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, nil, err
	}
	length := binary.LittleEndian.Uint32(header[:4])
	if length < fullMinLen {
		return nil, nil, ErrInvalidLength
	}
	payloadLen := int(length) - fullMinLen
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, nil, err
	}
	var crcBuf [4]byte
	if _, err := io.ReadFull(r, crcBuf[:]); err != nil {
		return nil, nil, err
	}
	crcExpected := binary.LittleEndian.Uint32(crcBuf[:])
	crc := crc32.NewIEEE()
	_, _ = crc.Write(header[:])
	_, _ = crc.Write(payload)
	if crc.Sum32() != crcExpected {
		return nil, nil, ErrInvalidLength
	}
	if err := readTransportError(payload); err != nil {
		return nil, nil, err
	}
	return payload, nil, nil
}

func (f *Full) WritePacket(w io.Writer, payload []byte) error {
	f.mu.Lock()
	seq := f.next
	f.next++
	f.mu.Unlock()
	return writeFullPacket(w, seq, payload)
}
