package tl

import (
	"bytes"
	"compress/gzip"
	"io"
	"testing"

	"github.com/r6m/tlrpc/mtproto"
)

func TestMsgsAckRoundTrip(t *testing.T) {
	in := &MsgsAck{MsgIDs: []int64{1, 2, 3}}
	buf := &bytes.Buffer{}
	if err := in.SerializeTL(buf); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if got := bytesToUint32(buf.Bytes()[:4]); got != MsgsAckID {
		t.Fatalf("constructor id: got %08x want %08x", got, MsgsAckID)
	}
	out := &MsgsAck{}
	if err := out.DeserializeTL(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	if len(out.MsgIDs) != 3 || out.MsgIDs[2] != 3 {
		t.Fatalf("unexpected msg ids: %+v", out.MsgIDs)
	}
}

func TestMsgContainerRoundTrip(t *testing.T) {
	ack := &MsgsAck{MsgIDs: []int64{42}}
	c := &MsgContainer{Messages: []Message{{MsgID: 10, SeqNo: 1, Body: ack}}}

	buf := &bytes.Buffer{}
	if err := c.SerializeTL(buf); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if got := bytesToUint32(buf.Bytes()[:4]); got != MsgContainerID {
		t.Fatalf("constructor id: got %08x want %08x", got, MsgContainerID)
	}

	out := &MsgContainer{}
	if err := out.DeserializeTL(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	if len(out.Messages) != 1 {
		t.Fatalf("messages len: %d", len(out.Messages))
	}
	if bytesToUint32(out.Messages[0].BodyRaw[:4]) != MsgsAckID {
		t.Fatalf("inner body constructor mismatch")
	}
}

func TestRPCResultWithErrorRoundTrip(t *testing.T) {
	res := &RPCResult{ReqMsgID: 99, Result: &RPCError{ErrorCode: 400, ErrorMessage: "BAD_REQUEST"}}
	buf := &bytes.Buffer{}
	if err := res.SerializeTL(buf); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if got := bytesToUint32(buf.Bytes()[:4]); got != RPCResultID {
		t.Fatalf("constructor id: got %08x want %08x", got, RPCResultID)
	}
	out := &RPCResult{}
	if err := out.DeserializeTL(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	if out.ReqMsgID != 99 {
		t.Fatalf("req msg id: %d", out.ReqMsgID)
	}
	if bytesToUint32(out.ResultRaw[:4]) != RPCErrorID {
		t.Fatalf("result constructor mismatch")
	}
}

func TestGzipPackedRoundTrip(t *testing.T) {
	inner := &MsgsAck{MsgIDs: []int64{7}}
	innerBuf := &bytes.Buffer{}
	if err := inner.SerializeTL(innerBuf); err != nil {
		t.Fatalf("serialize inner: %v", err)
	}
	packed := &bytes.Buffer{}
	zw := gzip.NewWriter(packed)
	if _, err := zw.Write(innerBuf.Bytes()); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	in := &GzipPacked{PackedData: packed.Bytes()}
	buf := &bytes.Buffer{}
	if err := in.SerializeTL(buf); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	out := &GzipPacked{}
	if err := out.DeserializeTL(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	if len(out.PackedData) == 0 {
		t.Fatal("empty packed data")
	}
}

func TestGetFutureSaltsRoundTrip(t *testing.T) {
	in := &GetFutureSaltsRequest{Num: 32}
	buf := &bytes.Buffer{}
	if err := in.SerializeTL(buf); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if got := bytesToUint32(buf.Bytes()[:4]); got != GetFutureSaltsID {
		t.Fatalf("constructor id: got %08x want %08x", got, GetFutureSaltsID)
	}
	out := &GetFutureSaltsRequest{}
	if err := out.DeserializeTL(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	if out.Num != 32 {
		t.Fatalf("num: got %d want %d", out.Num, 32)
	}
}

func TestFutureSaltsRoundTripUsesBareVector(t *testing.T) {
	in := &FutureSalts{
		ReqMsgID: 77,
		Now:      1234,
		Salts: []FutureSalt{
			{ValidSince: 1, ValidUntil: 2, Salt: 3},
		},
	}
	buf := &bytes.Buffer{}
	if err := in.SerializeTL(buf); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	raw := buf.Bytes()
	if got := bytesToUint32(raw[:4]); got != FutureSaltsID {
		t.Fatalf("constructor id: got %08x want %08x", got, FutureSaltsID)
	}

	// salts is encoded as bare vector length (no vector constructor ID).
	// Offset: constructor(4) + req_msg_id(8) + now(4) = 16
	if got := bytesToUint32(raw[16:20]); got != 1 {
		t.Fatalf("salts length: got %d want 1", got)
	}

	out := &FutureSalts{}
	if err := out.DeserializeTL(bytes.NewReader(raw)); err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	if out.ReqMsgID != 77 || out.Now != 1234 {
		t.Fatalf("header mismatch: %+v", out)
	}
	if len(out.Salts) != 1 || out.Salts[0].Salt != 3 {
		t.Fatalf("unexpected salts: %+v", out.Salts)
	}
}

func TestRPCDropAnswerRoundTrip(t *testing.T) {
	in := &RPCDropAnswer{ReqMsgID: 12345}
	buf := &bytes.Buffer{}
	if err := in.SerializeTL(buf); err != nil {
		t.Fatalf("serialize request: %v", err)
	}
	out := &RPCDropAnswer{}
	if err := out.DeserializeTL(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("deserialize request: %v", err)
	}
	if out.ReqMsgID != in.ReqMsgID {
		t.Fatalf("req_msg_id = %d, want %d", out.ReqMsgID, in.ReqMsgID)
	}

	unknown := &RPCAnswerUnknown{}
	buf.Reset()
	if err := unknown.SerializeTL(buf); err != nil {
		t.Fatalf("serialize response: %v", err)
	}
	if err := (&RPCAnswerUnknown{}).DeserializeTL(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("deserialize response: %v", err)
	}
}

func TestNestedContainerDecodingSafety(t *testing.T) {
	buf := &bytes.Buffer{}
	if err := mtproto.WriteUint32(buf, MsgContainerID); err != nil {
		t.Fatalf("write container constructor: %v", err)
	}
	if err := mtproto.WriteVectorHeader(buf, 1); err != nil {
		t.Fatalf("write message vector: %v", err)
	}
	if err := mtproto.WriteInt64(buf, 1); err != nil {
		t.Fatalf("write child message id: %v", err)
	}
	if err := mtproto.WriteInt32(buf, 1); err != nil {
		t.Fatalf("write child sequence: %v", err)
	}
	if err := mtproto.WriteInt32(buf, 64); err != nil {
		t.Fatalf("write child body length: %v", err)
	}
	buf.WriteByte(0xaa)

	reader := newGuardedContainerReader(buf.Bytes(), 16)
	if err := (&MsgContainer{}).DeserializeTL(reader); err == nil {
		t.Fatal("msg_container accepted a child length larger than the remaining input")
	}
	if reader.oversizedRead {
		t.Errorf("msg_container requested more than %d bytes in one child-body read", reader.maxRead)
	}
}

type guardedContainerReader struct {
	reader        *bytes.Reader
	maxRead       int
	oversizedRead bool
}

func newGuardedContainerReader(data []byte, maxRead int) *guardedContainerReader {
	return &guardedContainerReader{reader: bytes.NewReader(data), maxRead: maxRead}
}

func (r *guardedContainerReader) Read(p []byte) (int, error) {
	if len(p) > r.maxRead {
		r.oversizedRead = true
		return 0, io.ErrUnexpectedEOF
	}
	return r.reader.Read(p)
}

func (r *guardedContainerReader) Len() int {
	return r.reader.Len()
}

func bytesToUint32(b []byte) uint32 {
	v, _ := mtproto.ReadUint32(bytes.NewReader(b))
	return v
}
