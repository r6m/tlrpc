package tlrpc

import (
	"bytes"
	"testing"

	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
)

func TestDecodeTLObject_MsgsAck(t *testing.T) {
	decoder := newDispatcher()
	decoder.RegisterConstructor(mtprototl.MsgsAckID, func() TLObject { return &mtprototl.MsgsAck{} })
	ack := &mtprototl.MsgsAck{MsgIDs: []int64{11, 22}}
	buf := &bytes.Buffer{}
	if err := ack.SerializeTL(buf); err != nil {
		t.Fatalf("serialize ack: %v", err)
	}
	obj, _, err := decodeTLObject(decoder, buf.Bytes())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	decoded, ok := obj.(*mtprototl.MsgsAck)
	if !ok {
		t.Fatalf("decoded type: %T", obj)
	}
	if len(decoded.MsgIDs) != 2 || decoded.MsgIDs[1] != 22 {
		t.Fatalf("decoded ids: %+v", decoded.MsgIDs)
	}
}
