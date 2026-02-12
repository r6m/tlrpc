package mtproto

import (
	"bytes"
	"testing"
)

func TestReadBoolInvalid(t *testing.T) {
	buf := &bytes.Buffer{}
	if err := WriteUint32(buf, 0xdeadbeef); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ReadBool(buf); err != ErrInvalidBool {
		t.Fatalf("expected invalid bool error")
	}
}

func TestReadBytesUnexpectedEOF(t *testing.T) {
	buf := bytes.NewBuffer([]byte{0x04, 0x01})
	if _, err := ReadBytes(buf); err == nil {
		t.Fatalf("expected EOF")
	}
}
