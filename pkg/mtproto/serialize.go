// Package mtproto provides TL serialization helpers.
package mtproto

import (
	"encoding/binary"
	"io"
)

// Serializer handles TL serialization.
type Serializer struct {
	writer io.Writer
}

// NewSerializer creates a new serializer.
func NewSerializer(w io.Writer) *Serializer {
	return &Serializer{writer: w}
}

// WriteInt32 writes a 32-bit integer.
func (s *Serializer) WriteInt32(value int32) error {
	return binary.Write(s.writer, binary.LittleEndian, value)
}

// WriteInt64 writes a 64-bit integer.
func (s *Serializer) WriteInt64(value int64) error {
	return binary.Write(s.writer, binary.LittleEndian, value)
}

// WriteBytes writes a byte slice with length prefix.
func (s *Serializer) WriteBytes(data []byte) error {
	if err := s.WriteInt32(int32(len(data))); err != nil {
		return err
	}
	_, err := s.writer.Write(data)
	return err
}

// WriteString writes a string.
func (s *Serializer) WriteString(str string) error {
	return s.WriteBytes([]byte(str))
}

// WriteVector writes a vector of items.
func (s *Serializer) WriteVector(items []interface{}, writeItem func(interface{}) error) error {
	if err := s.WriteInt32(int32(len(items))); err != nil {
		return err
	}
	for _, item := range items {
		if err := writeItem(item); err != nil {
			return err
		}
	}
	return nil
}