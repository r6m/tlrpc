package tlrpc

import (
	"bytes"
	"io"
	"testing"

	"github.com/r6m/tlrpc/mtproto"
)

type testTL struct {
	id uint32
}

func (t *testTL) ConstructorID() uint32 { return t.id }
func (t *testTL) Method() string        { return "" }
func (t *testTL) TLName() string        { return "test" }

func (t *testTL) SerializeTL(w io.Writer) error {
	return mtproto.WriteUint32(w, t.id)
}

func (t *testTL) DeserializeTL(io.Reader) error {
	return nil
}

func TestNormalizeResponseDerefInterfacePointer(t *testing.T) {
	var obj TLObject = &testTL{id: 0x01020304}
	ptr := &obj

	got, err := normalizeResponse(ptr)
	if err != nil {
		t.Fatalf("normalizeResponse error: %v", err)
	}
	if got != obj {
		t.Fatalf("unexpected object: got %v want %v", got, obj)
	}

	var nilObj TLObject
	ptr = &nilObj
	got, err = normalizeResponse(ptr)
	if err != nil {
		t.Fatalf("normalizeResponse nil error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil response, got %v", got)
	}
}

func TestNormalizeResponseVectorEncoding(t *testing.T) {
	resp := []TLObject{&testTL{id: 0x11111111}, &testTL{id: 0x22222222}}
	obj, err := normalizeResponse(resp)
	if err != nil {
		t.Fatalf("normalizeResponse error: %v", err)
	}
	data, err := encodeTLObject(obj)
	if err != nil {
		t.Fatalf("encodeTLObject error: %v", err)
	}

	buf := &bytes.Buffer{}
	if err := mtproto.WriteVectorHeader(buf, 2); err != nil {
		t.Fatalf("write vector header: %v", err)
	}
	if err := mtproto.WriteUint32(buf, 0x11111111); err != nil {
		t.Fatalf("write item1: %v", err)
	}
	if err := mtproto.WriteUint32(buf, 0x22222222); err != nil {
		t.Fatalf("write item2: %v", err)
	}

	if !bytes.Equal(data, buf.Bytes()) {
		t.Fatalf("vector encoding mismatch: got %x want %x", data, buf.Bytes())
	}
}

func TestNormalizeResponseVectorPrimitives(t *testing.T) {
	resp := []int32{10, 20}
	obj, err := normalizeResponse(resp)
	if err != nil {
		t.Fatalf("normalizeResponse error: %v", err)
	}
	data, err := encodeTLObject(obj)
	if err != nil {
		t.Fatalf("encodeTLObject error: %v", err)
	}

	buf := &bytes.Buffer{}
	if err := mtproto.WriteVectorHeader(buf, 2); err != nil {
		t.Fatalf("write vector header: %v", err)
	}
	if err := mtproto.WriteInt32(buf, 10); err != nil {
		t.Fatalf("write item1: %v", err)
	}
	if err := mtproto.WriteInt32(buf, 20); err != nil {
		t.Fatalf("write item2: %v", err)
	}

	if !bytes.Equal(data, buf.Bytes()) {
		t.Fatalf("vector primitive encoding mismatch: got %x want %x", data, buf.Bytes())
	}
}
