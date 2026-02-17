package tl

import (
	"bytes"
	"io"
)

// Object is the minimal interface for TL objects in mtproto/tl.
type Object interface {
	ConstructorID() uint32
	SerializeTL(io.Writer) error
	DeserializeTL(io.Reader) error
}

func serializeObject(obj Object) ([]byte, error) {
	buf := &bytes.Buffer{}
	if err := obj.SerializeTL(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
