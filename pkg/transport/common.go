// Package transport provides common utilities for transports.
package transport

import (
	"encoding/binary"
	"errors"
	"io"
)

// Common errors
var (
	ErrInvalidMessage = errors.New("transport: invalid message")
	ErrMessageTooLarge = errors.New("transport: message too large")
)

// MessageReader reads framed messages from a stream.
type MessageReader struct {
	reader io.Reader
}

// NewMessageReader creates a new message reader.
func NewMessageReader(r io.Reader) *MessageReader {
	return &MessageReader{reader: r}
}

// ReadMessage reads a single message.
func (r *MessageReader) ReadMessage() ([]byte, error) {
	// Read message length (4 bytes, little endian)
	var length uint32
	if err := binary.Read(r.reader, binary.LittleEndian, &length); err != nil {
		return nil, err
	}

	// Check message size
	if length > 10*1024*1024 { // 10MB limit
		return nil, ErrMessageTooLarge
	}

	// Read message data
	data := make([]byte, length)
	if _, err := io.ReadFull(r.reader, data); err != nil {
		return nil, err
	}

	return data, nil
}

// MessageWriter writes framed messages to a stream.
type MessageWriter struct {
	writer io.Writer
}

// NewMessageWriter creates a new message writer.
func NewMessageWriter(w io.Writer) *MessageWriter {
	return &MessageWriter{writer: w}
}

// WriteMessage writes a single message.
func (w *MessageWriter) WriteMessage(data []byte) error {
	// Write message length (4 bytes, little endian)
	length := uint32(len(data))
	if err := binary.Write(w.writer, binary.LittleEndian, length); err != nil {
		return err
	}

	// Write message data
	_, err := w.writer.Write(data)
	return err
}