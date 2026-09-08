package runtime

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
)

func TestConnectionAndroidEmulatorInitReachesApplication(t *testing.T) {
	const method = uint32(0x10101010)
	requestID := inboundMessageID(4)
	init := encodeControlBody(t, &mtprototl.InitConnection{
		Flags: 2, APIID: 100001, DeviceModel: "Android", SystemVersion: "16",
		AppVersion: "1", SystemLangCode: "en", LangPack: "android", LangCode: "en",
		Params: &mtprototl.JSONValue{Kind: mtprototl.JSONObjectID, Object: []mtprototl.JSONObjectValue{
			{Key: "tz_offset", Value: mtprototl.JSONValue{Kind: mtprototl.JSONNumberID, Number: 12600}},
		}}, QueryRaw: constructorBody(method),
	})
	// Use Android's wire flag directly so the test does not depend on the
	// server serializer accepting it before exercising the decoder.
	binary.LittleEndian.PutUint32(init[4:], 1026)
	wrapped := encodeControlBody(t, &mtprototl.InvokeWithLayer{Layer: 229, QueryRaw: init})
	application := &connectionApplicationStub{outcome: Outcome{Intents: []Intent{
		RPCResult{RequestMessageID: requestID, Body: constructorBody(0x20202020)},
	}}}
	harness := newConnectionHarness(t, time.Unix(inboundNowSeconds, 0), application, 2, []mtproto.InnerData{{
		Salt: inboundSalt, SessionID: inboundSessionID, MsgID: requestID, SeqNo: 1, Data: wrapped,
	}})
	if err := harness.connection.Run(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("Android init closed connection before reply: %v", err)
	}
	if application.request.Message.ConstructorID != method {
		t.Fatalf("application constructor = %#x", application.request.Message.ConstructorID)
	}
	frames := harness.transport.writtenFrames()
	if len(frames) != 2 {
		t.Fatalf("frames = %d, want new session and RPC response", len(frames))
	}
	var container mtprototl.MsgContainer
	if err := decodeControl(decryptWriterFrame(t, harness.authKey, frames[1]).Data, &container); err != nil {
		t.Fatal(err)
	}
	var result mtprototl.RPCResult
	if len(container.Messages) != 2 {
		t.Fatalf("response children = %d", len(container.Messages))
	}
	if err := decodeControl(container.Messages[0].BodyRaw, &result); err != nil {
		t.Fatal(err)
	}
	if result.ReqMsgID != requestID || binaryConstructor(result.ResultRaw) != 0x20202020 {
		t.Fatalf("unexpected response: %+v", result)
	}
}
