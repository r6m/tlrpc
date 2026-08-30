package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
)

func TestMTProtoControlRouterConsumesAcknowledgements(t *testing.T) {
	outbound := &controlOutboundStub{}
	router := newControlRouter(t, outbound, &controlInboundStub{})
	body := encodeControlBody(t, &mtprototl.MsgsAck{MsgIDs: []int64{11, 13}})
	outcome, handled, err := router.RouteControl(context.Background(), controlRequest(101, body))
	if err != nil || !handled || len(outcome.Intents) != 0 {
		t.Fatalf("route acknowledgement = %+v, %t, %v", outcome, handled, err)
	}
	if !reflect.DeepEqual(outbound.acknowledged, []int64{11, 13}) {
		t.Fatalf("acknowledged IDs = %v", outbound.acknowledged)
	}
}

func TestMTProtoControlRouterReportsInboundState(t *testing.T) {
	inbound := &controlInboundStub{info: []byte{132, 1}}
	router := newControlRouter(t, &controlOutboundStub{}, inbound)
	body := encodeControlBody(t, &mtprototl.MsgsStateReq{MsgIDs: []int64{41, 37}})
	outcome, handled, err := router.RouteControl(context.Background(), controlRequest(105, body))
	if err != nil || !handled || len(outcome.Intents) != 1 {
		t.Fatalf("route state request = %+v, %t, %v", outcome, handled, err)
	}
	if !reflect.DeepEqual(inbound.requested, []int64{41, 37}) {
		t.Fatalf("queried IDs = %v", inbound.requested)
	}
	reply, ok := outcome.Intents[0].(ProtocolReply)
	if !ok || reply.ContentRelated {
		t.Fatalf("state reply intent = %#v", outcome.Intents[0])
	}
	decoded := &mtprototl.MsgsStateInfo{}
	if err := decodeControl(reply.Body, decoded); err != nil {
		t.Fatalf("decode state reply: %v", err)
	}
	if decoded.ReqMsgID != 105 || !bytes.Equal(decoded.Info, inbound.info) {
		t.Fatalf("state reply = %+v", decoded)
	}
}

func TestMTProtoControlRouterResendsOnlyWhenEveryIDIsEligible(t *testing.T) {
	outbound := &controlOutboundStub{states: map[int64]OutboundReliabilityState{
		11: {Known: true, ResendEligible: true},
		13: {Known: true, ResendEligible: true},
	}}
	router := newControlRouter(t, outbound, &controlInboundStub{})
	body := encodeControlBody(t, &mtprototl.MsgResendReq{MsgIDs: []int64{11, 13}})
	outcome, handled, err := router.RouteControl(context.Background(), controlRequest(109, body))
	if err != nil || !handled || len(outcome.Intents) != 1 {
		t.Fatalf("route resend = %+v, %t, %v", outcome, handled, err)
	}
	resend, ok := outcome.Intents[0].(Resend)
	if !ok || !reflect.DeepEqual(resend.MessageIDs, []int64{11, 13}) {
		t.Fatalf("resend intent = %#v", outcome.Intents[0])
	}
}

func TestMTProtoControlRouterFallsBackAtomicallyToOutboundState(t *testing.T) {
	outbound := &controlOutboundStub{states: map[int64]OutboundReliabilityState{
		11: {Known: true, ResendEligible: true},
		13: {Known: true, Acknowledged: true},
	}}
	router := newControlRouter(t, outbound, &controlInboundStub{})
	body := encodeControlBody(t, &mtprototl.MsgResendReq{MsgIDs: []int64{11, 13, 17}})
	outcome, handled, err := router.RouteControl(context.Background(), controlRequest(113, body))
	if err != nil || !handled || len(outcome.Intents) != 1 {
		t.Fatalf("route resend fallback = %+v, %t, %v", outcome, handled, err)
	}
	reply := outcome.Intents[0].(ProtocolReply)
	decoded := &mtprototl.MsgsStateInfo{}
	if err := decodeControl(reply.Body, decoded); err != nil {
		t.Fatal(err)
	}
	want := []byte{
		mtprototl.MessageStateReceived | mtprototl.MessageStateKnown,
		mtprototl.MessageStateReceived | mtprototl.MessageStateAcknowledged | mtprototl.MessageStateKnown,
		mtprototl.MessageStateUnknownTooOld,
	}
	if decoded.ReqMsgID != 113 || !bytes.Equal(decoded.Info, want) {
		t.Fatalf("fallback state = %+v, want %v", decoded, want)
	}
}

func TestMTProtoControlRouterRejectsTrailingDataAndDelegatesApplication(t *testing.T) {
	router := newControlRouter(t, &controlOutboundStub{}, &controlInboundStub{})
	body := append(encodeControlBody(t, &mtprototl.MsgsAck{MsgIDs: []int64{11}}), 0xff)
	_, handled, err := router.RouteControl(context.Background(), controlRequest(117, body))
	if !handled || !errors.Is(err, ErrTrailingControlData) {
		t.Fatalf("trailing control = handled %t error %v", handled, err)
	}

	unknown := constructorBody(0x01020304)
	outcome, handled, err := router.RouteControl(context.Background(), controlRequest(121, unknown))
	if err != nil || handled || len(outcome.Intents) != 0 {
		t.Fatalf("unknown control = %+v, %t, %v", outcome, handled, err)
	}
}

func TestMTProtoControlRouterDropsRunningRequestAndCorrelatesAnswer(t *testing.T) {
	active := newActiveRequestRegistryForTest(t, 1)
	handlerCtx, complete, err := active.Begin(context.Background(), 77)
	if err != nil {
		t.Fatal(err)
	}
	defer complete()
	router, err := NewMTProtoControlRouter(MTProtoControlConfig{Outbound: &controlOutboundStub{}, Inbound: &controlInboundStub{}, Active: active})
	if err != nil {
		t.Fatal(err)
	}
	body := encodeControlBody(t, &mtprototl.RPCDropAnswer{ReqMsgID: 77})
	outcome, handled, err := router.RouteControl(context.Background(), controlRequest(125, body))
	if err != nil || !handled || len(outcome.Intents) != 1 {
		t.Fatalf("route rpc_drop_answer = %+v, %t, %v", outcome, handled, err)
	}
	result := outcome.Intents[0].(RPCResult)
	if result.RequestMessageID != 125 || binaryConstructor(result.Body) != mtprototl.RPCAnswerDroppedRunningID {
		t.Fatalf("drop result = %#v", result)
	}
	if !errors.Is(handlerCtx.Err(), context.Canceled) {
		t.Fatalf("running handler was not canceled: %v", handlerCtx.Err())
	}

	body = encodeControlBody(t, &mtprototl.RPCDropAnswer{ReqMsgID: 77})
	outcome, _, err = router.RouteControl(context.Background(), controlRequest(129, body))
	if err != nil {
		t.Fatal(err)
	}
	result = outcome.Intents[0].(RPCResult)
	if result.RequestMessageID != 129 || binaryConstructor(result.Body) != mtprototl.RPCAnswerUnknownID {
		t.Fatalf("repeated drop result = %#v", result)
	}
}

func TestMTProtoControlRouterReturnsBoundedCurrentFutureSalts(t *testing.T) {
	active := newActiveRequestRegistryForTest(t, 1)
	now := time.Unix(1_700_000_000, 0)
	router, err := NewMTProtoControlRouter(MTProtoControlConfig{
		Outbound: &controlOutboundStub{}, Inbound: &controlInboundStub{}, Active: active,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	body := encodeControlBody(t, &mtprototl.GetFutureSaltsRequest{Num: 1000})
	request := controlRequest(133, body)
	request.Info.ServerSalt = 9876
	outcome, handled, err := router.RouteControl(context.Background(), request)
	if err != nil || !handled || len(outcome.Intents) != 1 {
		t.Fatalf("route future salts = %+v, %t, %v", outcome, handled, err)
	}
	reply := outcome.Intents[0].(ProtocolReply)
	salts := &mtprototl.FutureSalts{}
	if err := decodeControl(reply.Body, salts); err != nil {
		t.Fatal(err)
	}
	if salts.ReqMsgID != 133 || salts.Now != int32(now.Unix()) || len(salts.Salts) != mtprototl.MaxFutureSalts {
		t.Fatalf("future salts = %+v", salts)
	}
	for _, salt := range salts.Salts {
		if salt.Salt != 9876 {
			t.Fatalf("future salt value = %d, want current salt", salt.Salt)
		}
	}
}

type controlOutboundStub struct {
	acknowledged []int64
	states       map[int64]OutboundReliabilityState
}

func (s *controlOutboundStub) AcknowledgeOutbound(_ context.Context, ids []int64) error {
	s.acknowledged = append([]int64(nil), ids...)
	return nil
}

func (s *controlOutboundStub) InspectOutbound(_ context.Context, id int64) (OutboundReliabilityState, error) {
	return s.states[id], nil
}

type controlInboundStub struct {
	requested []int64
	info      []byte
}

func (s *controlInboundStub) StateInfo(ids []int64) []byte {
	s.requested = append([]int64(nil), ids...)
	return append([]byte(nil), s.info...)
}

func newControlRouter(t *testing.T, outbound OutboundReliability, inbound InboundStateSource) *MTProtoControlRouter {
	t.Helper()
	active, err := NewActiveRequestRegistry(4)
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewMTProtoControlRouter(MTProtoControlConfig{Outbound: outbound, Inbound: inbound, Active: active})
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func controlRequest(messageID int64, body []byte) Request {
	return Request{Message: InboundMessage{
		MessageID: messageID, SequenceNo: 2,
		ConstructorID:  binaryConstructor(body),
		Body:           body,
		ContentRelated: false,
	}}
}

func binaryConstructor(body []byte) uint32 {
	return uint32(body[0]) | uint32(body[1])<<8 | uint32(body[2])<<16 | uint32(body[3])<<24
}

func encodeControlBody(t *testing.T, value interface{ SerializeTL(io.Writer) error }) []byte {
	t.Helper()
	body, err := serializeRuntimeTL(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
