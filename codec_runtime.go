package tlrpc

import (
	"bytes"
	"fmt"
	"io"
)

func decodeTLObject(d *dispatcher, data []byte) (TLObject, *bytes.Reader, error) {
	if len(data) < 4 {
		return nil, nil, io.ErrUnexpectedEOF
	}
	constructorID := mtprotoReadUint32Bytes(data[:4])
	constructor, ok := d.LookupConstructor(constructorID)
	if !ok {
		return nil, nil, NewNotFoundError("UNKNOWN_CONSTRUCTOR")
	}
	obj := constructor()
	r := bytes.NewReader(data)
	deser, ok := obj.(interface{ DeserializeTL(io.Reader) error })
	if !ok {
		return nil, nil, fmt.Errorf("constructor %08x does not implement DeserializeTL", constructorID)
	}
	if err := deser.DeserializeTL(r); err != nil {
		return nil, nil, err
	}
	return obj, r, nil
}

func mtprotoReadUint32Bytes(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
