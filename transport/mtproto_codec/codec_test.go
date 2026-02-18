package mtprotocodec

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestAbridgedCodecRoundTrip(t *testing.T) {
	codec := &Abridged{}
	payload := bytes.Repeat([]byte{0x11}, 8)
	buf := &bytes.Buffer{}
	if err := codec.WritePacket(buf, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, _, err := codec.ReadPacket(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch")
	}
}

func TestIntermediateCodecRoundTrip(t *testing.T) {
	codec := &Intermediate{}
	payload := bytes.Repeat([]byte{0x22}, 12)
	buf := &bytes.Buffer{}
	if err := codec.WritePacket(buf, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, _, err := codec.ReadPacket(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch")
	}
}

func TestPaddedIntermediateCodecRoundTrip(t *testing.T) {
	codec := &PaddedIntermediate{}
	payload := bytes.Repeat([]byte{0x33}, 12)
	buf := &bytes.Buffer{}
	if err := codec.WritePacket(buf, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, _, err := codec.ReadPacket(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) < len(payload) {
		t.Fatalf("payload shorter than expected")
	}
	if !bytes.Equal(got[:len(payload)], payload) {
		t.Fatalf("payload prefix mismatch")
	}
}

func TestFullCodecRoundTrip(t *testing.T) {
	codec := &Full{}
	payload := bytes.Repeat([]byte{0x44}, 16)
	buf := &bytes.Buffer{}
	if err := codec.WritePacket(buf, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, _, err := codec.ReadPacket(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch")
	}
}

func TestIntermediateQuickAck(t *testing.T) {
	codec := &Intermediate{AllowQuickAckTokens: true}
	buf := &bytes.Buffer{}
	var token uint32 = 0x1234
	binary.Write(buf, binary.LittleEndian, token|0x80000000)
	_, got, err := codec.ReadPacket(bytes.NewReader(buf.Bytes()))
	if err != ErrQuickAck || got == nil || *got != token {
		t.Fatalf("expected quick ack token")
	}
}
