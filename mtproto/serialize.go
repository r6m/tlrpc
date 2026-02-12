package mtproto

import (
	"encoding/binary"
	"io"
	"math"
)

const (
	BoolTrue  uint32 = 0x997275b5
	BoolFalse uint32 = 0xbc799737

	VectorConstructorID uint32 = 0x1cb5c415
)

// WriteInt32 writes an int32 in little-endian.
func WriteInt32(w io.Writer, v int32) error {
	return WriteUint32(w, uint32(v))
}

// WriteUint32 writes a uint32 in little-endian.
func WriteUint32(w io.Writer, v uint32) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	_, err := w.Write(buf[:])
	return err
}

// WriteInt64 writes an int64 in little-endian.
func WriteInt64(w io.Writer, v int64) error {
	return WriteUint64(w, uint64(v))
}

// WriteUint64 writes a uint64 in little-endian.
func WriteUint64(w io.Writer, v uint64) error {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	_, err := w.Write(buf[:])
	return err
}

// WriteInt128 writes a 16-byte integer.
func WriteInt128(w io.Writer, v [16]byte) error {
	_, err := w.Write(v[:])
	return err
}

// WriteInt256 writes a 32-byte integer.
func WriteInt256(w io.Writer, v [32]byte) error {
	_, err := w.Write(v[:])
	return err
}

// WriteDouble writes a float64 in little-endian IEEE 754.
func WriteDouble(w io.Writer, v float64) error {
	return WriteUint64(w, math.Float64bits(v))
}

// WriteString writes a TL string.
func WriteString(w io.Writer, v string) error {
	return WriteBytes(w, []byte(v))
}

// WriteBytes writes TL bytes with length prefix and padding.
func WriteBytes(w io.Writer, v []byte) error {
	length := len(v)
	if length < 254 {
		if _, err := w.Write([]byte{byte(length)}); err != nil {
			return err
		}
		if _, err := w.Write(v); err != nil {
			return err
		}
		padding := (4 - ((1 + length) % 4)) % 4
		if padding > 0 {
			_, err := w.Write(make([]byte, padding))
			return err
		}
		return nil
	}

	if length > math.MaxInt32 {
		return ErrStringTooLong
	}

	var header [4]byte
	header[0] = 0xFE
	header[1] = byte(length)
	header[2] = byte(length >> 8)
	header[3] = byte(length >> 16)
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if _, err := w.Write(v); err != nil {
		return err
	}
	padding := (4 - ((4 + length) % 4)) % 4
	if padding > 0 {
		_, err := w.Write(make([]byte, padding))
		return err
	}
	return nil
}

// WriteBool writes a TL Bool.
func WriteBool(w io.Writer, v bool) error {
	if v {
		return WriteUint32(w, BoolTrue)
	}
	return WriteUint32(w, BoolFalse)
}

// WriteVectorHeader writes the vector constructor and count.
func WriteVectorHeader(w io.Writer, count int) error {
	if err := WriteUint32(w, VectorConstructorID); err != nil {
		return err
	}
	return WriteInt32(w, int32(count))
}
