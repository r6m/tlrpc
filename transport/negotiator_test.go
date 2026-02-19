package transport

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"testing"

	mtprotocodec "github.com/r6m/tlrpc/transport/mtproto_codec"
)

func TestNegotiateAbridgedTag(t *testing.T) {
	payload := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	buf := bytes.NewBuffer(nil)
	buf.WriteByte(0xEF)
	buf.WriteByte(0x02) // 8 bytes / 4
	buf.Write(payload)

	r := bufio.NewReader(bytes.NewReader(buf.Bytes()))
	w := bufio.NewWriter(bytes.NewBuffer(nil))

	n := NewNegotiator(NegotiatorConfig{})
	rw, codec, err := n.Negotiate(r, w)
	if err != nil {
		t.Fatalf("negotiate: %v", err)
	}
	if _, ok := codec.(*mtprotocodec.Abridged); !ok {
		t.Fatalf("expected abridged codec, got %T", codec)
	}

	got, _, err := codec.ReadPacket(rw)
	if err != nil {
		t.Fatalf("read packet: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected payload: %x", got)
	}
}

func TestNegotiateIntermediateTag(t *testing.T) {
	payload := []byte{0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17}
	buf := bytes.NewBuffer(nil)
	binary.Write(buf, binary.LittleEndian, uint32(0xEEEEEEEE))
	binary.Write(buf, binary.LittleEndian, uint32(len(payload)))
	buf.Write(payload)

	r := bufio.NewReader(bytes.NewReader(buf.Bytes()))
	w := bufio.NewWriter(bytes.NewBuffer(nil))

	n := NewNegotiator(NegotiatorConfig{})
	rw, codec, err := n.Negotiate(r, w)
	if err != nil {
		t.Fatalf("negotiate: %v", err)
	}
	if _, ok := codec.(*mtprotocodec.Intermediate); !ok {
		t.Fatalf("expected intermediate codec, got %T", codec)
	}

	got, _, err := codec.ReadPacket(rw)
	if err != nil {
		t.Fatalf("read packet: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected payload: %x", got)
	}
}

func TestNegotiatePaddedIntermediateTag(t *testing.T) {
	payload := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x11, 0x22}
	buf := bytes.NewBuffer(nil)
	binary.Write(buf, binary.LittleEndian, uint32(0xDDDDDDDD))
	binary.Write(buf, binary.LittleEndian, uint32(len(payload)))
	buf.Write(payload)

	r := bufio.NewReader(bytes.NewReader(buf.Bytes()))
	w := bufio.NewWriter(bytes.NewBuffer(nil))

	n := NewNegotiator(NegotiatorConfig{})
	rw, codec, err := n.Negotiate(r, w)
	if err != nil {
		t.Fatalf("negotiate: %v", err)
	}
	if _, ok := codec.(*mtprotocodec.PaddedIntermediate); !ok {
		t.Fatalf("expected padded intermediate codec, got %T", codec)
	}

	got, _, err := codec.ReadPacket(rw)
	if err != nil {
		t.Fatalf("read packet: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected payload: %x", got)
	}
}

func TestIntermediateWriteDoesNotEchoTag(t *testing.T) {
	payload := []byte{0x01, 0x02, 0x03, 0x04}
	buf := bytes.NewBuffer(nil)
	codec := &mtprotocodec.Intermediate{}
	if err := codec.WritePacket(buf, payload); err != nil {
		t.Fatalf("write packet: %v", err)
	}
	data := buf.Bytes()
	if len(data) < 4 {
		t.Fatalf("short write: %d", len(data))
	}
	length := binary.LittleEndian.Uint32(data[:4])
	if length != uint32(len(payload)) {
		t.Fatalf("unexpected length: %d", length)
	}
	if bytes.Equal(data[:4], []byte{0xEE, 0xEE, 0xEE, 0xEE}) {
		t.Fatalf("intermediate tag should not be echoed")
	}
}
