package tlrpc

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/r6m/tlrpc/mtproto"
)

const DefaultMaxEncodedTLBytes = 16 << 20

var ErrEncodedTLTooLarge = errors.New("tlrpc: encoded TL object exceeds limit")

type EncodeLimits struct {
	MaxEncodedBytes int
}

func encodeTLObject(obj TLObject) ([]byte, error) {
	return encodeTLObjectWithLimits(obj, EncodeLimits{})
}

func encodeTLObjectWithLimits(obj TLObject, limits EncodeLimits) ([]byte, error) {
	if obj == nil {
		return nil, fmt.Errorf("tlrpc: cannot encode nil TL object")
	}
	maxBytes := limits.MaxEncodedBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxEncodedTLBytes
	}
	if maxBytes < 0 {
		return nil, fmt.Errorf("tlrpc: encoded TL limit must not be negative")
	}
	serializer, ok := obj.(interface{ SerializeTL(io.Writer) error })
	if !ok {
		return nil, fmt.Errorf("constructor %08x does not implement SerializeTL", obj.ConstructorID())
	}
	var buffer bytes.Buffer
	writer := &boundedEncodeWriter{writer: &buffer, remaining: maxBytes}
	if err := serializer.SerializeTL(writer); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func decodeTLObject(d *dispatcher, data []byte) (TLObject, *bytes.Reader, error) {
	return decodeTLObjectWithLimits(d, data, mtproto.DecodeLimits{})
}

func decodeTLObjectWithLimits(d *dispatcher, data []byte, limits mtproto.DecodeLimits) (TLObject, *bytes.Reader, error) {
	if len(data) < 4 {
		return nil, nil, io.ErrUnexpectedEOF
	}
	budget, err := mtproto.NewDecodeBudget(limits)
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > limitsOrDefaultDecodedBytes(limits) {
		return nil, nil, mtproto.ErrDecodedBytesLimit
	}
	return decodeTLObjectWithBudget(d, data, budget)
}

func decodeTLObjectWithBudget(d *dispatcher, data []byte, budget *mtproto.DecodeBudget) (TLObject, *bytes.Reader, error) {
	if len(data) < 4 {
		return nil, nil, io.ErrUnexpectedEOF
	}
	if budget == nil {
		var err error
		budget, err = mtproto.NewDecodeBudget(mtproto.DecodeLimits{})
		if err != nil {
			return nil, nil, err
		}
	}
	unlockBudget := budget.LockDecode()
	defer unlockBudget()
	constructorID := mtprotoReadUint32Bytes(data[:4])
	constructor, ok := d.LookupConstructor(constructorID)
	if !ok {
		return nil, nil, NewNotFoundError("UNKNOWN_CONSTRUCTOR")
	}
	obj := constructor()
	r := bytes.NewReader(data)
	reader := mtproto.NewBudgetReader(r, budget)
	deser, ok := obj.(interface{ DeserializeTL(io.Reader) error })
	if !ok {
		return nil, nil, fmt.Errorf("constructor %08x does not implement DeserializeTL", constructorID)
	}
	if err := deser.DeserializeTL(reader); err != nil {
		return nil, nil, err
	}
	return obj, r, nil
}

func limitsOrDefaultDecodedBytes(limits mtproto.DecodeLimits) int64 {
	if limits.MaxDecodedBytes > 0 {
		return limits.MaxDecodedBytes
	}
	return mtproto.DefaultMaxDecodedBytes
}

type boundedEncodeWriter struct {
	writer    io.Writer
	remaining int
}

func (w *boundedEncodeWriter) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		return 0, ErrEncodedTLTooLarge
	}
	n, err := w.writer.Write(p)
	w.remaining -= n
	return n, err
}

func mtprotoReadUint32Bytes(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
