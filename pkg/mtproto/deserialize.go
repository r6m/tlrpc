// Package mtproto provides TL deserialization helpers.
package mtproto

import (
	"encoding/binary"
	"io"
)

// Deserializer handles TL deserialization.
type Deserializer struct {
	reader io.Reader
}

// NewDeserializer creates a new deserializer.
func NewDeserializer(r io.Reader) *Deserializer {
	return &Deserializer{reader: r}
}

// ReadInt32 reads a 32-bit integer.
func (d *Deserializer) ReadInt32() (int32, error) {
	var value int32
	err := binary.Read(d.reader, binary.LittleEndian, &value)
	return value, err
}

// ReadInt64 reads a 64-bit integer.
func (d *Deserializer) ReadInt64() (int64, error) {
	var value int64
	err := binary.Read(d.reader, binary.LittleEndian, &value)
	return value, err
}

// ReadBytes reads a byte slice with length prefix.
func (d *Deserializer) ReadBytes() ([]byte, error) {
	length, err := d.ReadInt32()
	if err != nil {
		return nil, err
	}

	data := make([]byte, length)
	_, err = io.ReadFull(d.reader, data)
	return data, err
}

// ReadString reads a string.
func (d *Deserializer) ReadString() (string, error) {
	data, err := d.ReadBytes()
	return string(data), err
}

// ReadVector reads a vector of items.
func (d *Deserializer) ReadVector(readItem func() (interface{}, error)) ([]interface{}, error) {
	count, err := d.ReadInt32()
	if err != nil {
		return nil, err
	}

	items := make([]interface{}, count)
	for i := int32(0); i < count; i++ {
		item, err := readItem()
		if err != nil {
			return nil, err
		}
		items[i] = item
	}

	return items, nil
}