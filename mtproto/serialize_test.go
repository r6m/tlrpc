package mtproto

import (
	"bytes"
	"math"
	"testing"
)

func TestSerializeRoundTripPrimitives(t *testing.T) {
	buf := &bytes.Buffer{}

	if err := WriteInt32(buf, -123); err != nil {
		t.Fatalf("write int32: %v", err)
	}
	if err := WriteInt64(buf, -456); err != nil {
		t.Fatalf("write int64: %v", err)
	}
	if err := WriteDouble(buf, math.Pi); err != nil {
		t.Fatalf("write double: %v", err)
	}
	if err := WriteBool(buf, true); err != nil {
		t.Fatalf("write bool: %v", err)
	}
	if err := WriteString(buf, "hello"); err != nil {
		t.Fatalf("write string: %v", err)
	}
	if err := WriteBytes(buf, []byte{0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("write bytes: %v", err)
	}

	int32Val, err := ReadInt32(buf)
	if err != nil || int32Val != -123 {
		t.Fatalf("read int32: %v %d", err, int32Val)
	}
	int64Val, err := ReadInt64(buf)
	if err != nil || int64Val != -456 {
		t.Fatalf("read int64: %v %d", err, int64Val)
	}
	doubleVal, err := ReadDouble(buf)
	if err != nil || doubleVal != math.Pi {
		t.Fatalf("read double: %v %f", err, doubleVal)
	}
	boolVal, err := ReadBool(buf)
	if err != nil || !boolVal {
		t.Fatalf("read bool: %v %v", err, boolVal)
	}
	strVal, err := ReadString(buf)
	if err != nil || strVal != "hello" {
		t.Fatalf("read string: %v %s", err, strVal)
	}
	bytesVal, err := ReadBytes(buf)
	if err != nil || !bytes.Equal(bytesVal, []byte{0x01, 0x02, 0x03}) {
		t.Fatalf("read bytes: %v %x", err, bytesVal)
	}
}

func TestSerializeBytesPadding(t *testing.T) {
	buf := &bytes.Buffer{}
	data := bytes.Repeat([]byte{0xAB}, 300)
	if err := WriteBytes(buf, data); err != nil {
		t.Fatalf("write bytes: %v", err)
	}
	decoded, err := ReadBytes(buf)
	if err != nil {
		t.Fatalf("read bytes: %v", err)
	}
	if !bytes.Equal(decoded, data) {
		t.Fatalf("bytes mismatch")
	}
}

func TestVectorRoundTrip(t *testing.T) {
	buf := &bytes.Buffer{}
	if err := WriteVectorHeader(buf, 2); err != nil {
		t.Fatalf("write vector header: %v", err)
	}
	if err := WriteInt32(buf, 10); err != nil {
		t.Fatalf("write item: %v", err)
	}
	if err := WriteInt32(buf, 20); err != nil {
		t.Fatalf("write item: %v", err)
	}

	items := []int32{}
	if err := ReadVector(buf, func() error {
		v, err := ReadInt32(buf)
		if err != nil {
			return err
		}
		items = append(items, v)
		return nil
	}); err != nil {
		t.Fatalf("read vector: %v", err)
	}
	if len(items) != 2 || items[0] != 10 || items[1] != 20 {
		t.Fatalf("vector items mismatch")
	}
}
