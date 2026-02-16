package mtproto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
)

var (
	ErrInvalidBool   = errors.New("mtproto: invalid bool value")
	ErrStringTooLong = errors.New("mtproto: string too long")
)

// ReadInt32 reads an int32 in little-endian.
func ReadInt32(r io.Reader) (int32, error) {
	v, err := ReadUint32(r)
	return int32(v), err
}

// ReadUint32 reads a uint32 in little-endian.
func WriteBigInt(w io.Writer, n *big.Int, size int) error {
	data := n.Bytes()
	if len(data) < size {
		padded := make([]byte, size)
		copy(padded[size-len(data):], data)
		_, err := w.Write(padded)
		return err
	}
	_, err := w.Write(data[len(data)-size:])
	return err
}

func ReadUint32(r io.Reader) (uint32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf[:]), nil
}

// ReadInt64 reads an int64 in little-endian.
func ReadInt64(r io.Reader) (int64, error) {
	v, err := ReadUint64(r)
	return int64(v), err
}

// ReadUint64 reads a uint64 in little-endian.
func ReadUint64(r io.Reader) (uint64, error) {
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(buf[:]), nil
}

// ReadInt128 reads a 16-byte integer.
func ReadInt128(r io.Reader) ([16]byte, error) {
	var buf [16]byte
	_, err := io.ReadFull(r, buf[:])
	return buf, err
}

// ReadInt256 reads a 32-byte integer.
func ReadInt256(r io.Reader) ([32]byte, error) {
	var buf [32]byte
	_, err := io.ReadFull(r, buf[:])
	return buf, err
}

// ReadDouble reads a float64 in little-endian IEEE 754.
func ReadDouble(r io.Reader) (float64, error) {
	v, err := ReadUint64(r)
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(v), nil
}

// ReadString reads a TL string.
func ReadString(r io.Reader) (string, error) {
	b, err := ReadBytes(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ReadBytes reads TL bytes with length prefix and padding.
func ReadBytes(r io.Reader) ([]byte, error) {
	var sizeBuf [4]byte
	if _, err := io.ReadFull(r, sizeBuf[:1]); err != nil {
		return nil, err
	}
	first := sizeBuf[0]

	var length int
	var headerSize int
	if first < 254 {
		length = int(first)
		headerSize = 1
	} else {
		if _, err := io.ReadFull(r, sizeBuf[:3]); err != nil {
			return nil, err
		}
		sizeBuf[3] = 0
		length = int(binary.LittleEndian.Uint32(sizeBuf[:]))
		headerSize = 4
	}

	if length > math.MaxInt32 {
		return nil, ErrStringTooLong
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}

	padding := (4 - ((headerSize + length) % 4)) % 4
	if padding > 0 {
		var pad [3]byte
		if _, err := io.ReadFull(r, pad[:padding]); err != nil {
			return nil, err
		}
	}

	return data, nil
}

// ReadBool reads a TL Bool.
func ReadBool(r io.Reader) (bool, error) {
	v, err := ReadUint32(r)
	if err != nil {
		return false, err
	}
	if v == BoolTrue {
		return true, nil
	}
	if v == BoolFalse {
		return false, nil
	}
	return false, ErrInvalidBool
}

// ReadVector reads a TL vector and calls fn for each element.
func ReadVector(r io.Reader, fn func() error) error {
	ctor, err := ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != VectorConstructorID {
		return fmt.Errorf("mtproto: invalid vector constructor: %08x", ctor)
	}
	count, err := ReadInt32(r)
	if err != nil {
		return err
	}
	if count < 0 {
		return fmt.Errorf("mtproto: invalid vector length: %d", count)
	}
	for i := int32(0); i < count; i++ {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}
