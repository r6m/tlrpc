package compat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/compat/client"
	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/transport"
)

// These tests exercise TLRPC only through its transport and encrypted MTProto
// wire contract. Their expected behavior comes from tgserver's proven gateway
// conformance tests, not from TLRPC implementation details.

func TestEncryptedValidationReportsCanonicalBadMessageMetadata(t *testing.T) {
	for _, tc := range []struct {
		name      string
		messageID func() int64
		seqNo     int32
		wantCode  int32
	}{
		{
			name: "malformed or stale message id",
			messageID: func() int64 {
				return time.Now().Add(-time.Hour).Unix()<<32 | 4
			},
			seqNo:    7,
			wantCode: 16,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, cli := dialEncryptedHarness(t)
			payload := serializeCompatObject(t, &pingReq{Value: 10})
			msgID := tc.messageID()
			packet, err := cli.EncryptMessage(msgID, tc.seqNo, payload)
			if err != nil {
				t.Fatalf("encrypt request: %v", err)
			}
			if err := cli.Conn().WriteMessage(packet); err != nil {
				t.Fatalf("write request: %v", err)
			}

			_, obj := readUntilConstructor(t, cli, mtprototl.BadMsgNotificationID)
			bad := obj.(*mtprototl.BadMsgNotification)
			if bad.BadMsgID != msgID || bad.BadMsgSeq != tc.seqNo || bad.ErrorCode != tc.wantCode {
				t.Fatalf("bad_msg_notification = %+v, want id=%d seq=%d code=%d", bad, msgID, tc.seqNo, tc.wantCode)
			}
		})
	}
}

func TestEncryptedValidationRejectsReplayWithCode32(t *testing.T) {
	_, cli := dialEncryptedHarness(t)
	payload := serializeCompatObject(t, &pingReq{Value: 20})
	msgID := client.NextMsgID()
	writeEncryptedRequest(t, cli, msgID, 1, payload)
	drainRPCExchange(t, cli, msgID, true)

	writeEncryptedRequest(t, cli, msgID, 1, payload)
	_, obj := readUntilConstructor(t, cli, mtprototl.BadMsgNotificationID)
	bad := obj.(*mtprototl.BadMsgNotification)
	if bad.BadMsgID != msgID || bad.BadMsgSeq != 1 || bad.ErrorCode != 32 {
		t.Fatalf("replay notification = %+v, want id=%d seq=1 code=32", bad, msgID)
	}
}

func TestEncryptedValidationAcceptsRestoredHighSequenceSession(t *testing.T) {
	h, cli := dialEncryptedHarness(t)
	payload := serializeCompatObject(t, &pingReq{Value: 30})
	msgID := client.NextMsgID()
	packet, err := cli.EncryptMessageWithSession(h.salt, h.session+1, msgID, 3, payload)
	if err != nil {
		t.Fatalf("encrypt request: %v", err)
	}
	if err := cli.Conn().WriteMessage(packet); err != nil {
		t.Fatalf("write request: %v", err)
	}
	cli.SetSession(h.keyID, h.key, h.salt, h.session+1)
	responses := drainRPCExchange(t, cli, msgID, true)
	rpcResult := responses[mtprototl.RPCResultID].(*mtprototl.RPCResult)
	var result pingResp
	if err := result.DeserializeTL(bytes.NewReader(rpcResult.ResultRaw)); err != nil || result.Value != 31 {
		t.Fatalf("restored-session result = %+v, %v; want ping result 31", result, err)
	}
}

func TestEncryptedValidationReportsCanonicalBadServerSalt(t *testing.T) {
	h, cli := dialEncryptedHarness(t)
	payload := serializeCompatObject(t, &pingReq{Value: 40})
	msgID := client.NextMsgID()
	packet, err := cli.EncryptMessageWithSalt(h.salt+1, msgID, 5, payload)
	if err != nil {
		t.Fatalf("encrypt request: %v", err)
	}
	if err := cli.Conn().WriteMessage(packet); err != nil {
		t.Fatalf("write request: %v", err)
	}

	_, obj := readUntilConstructor(t, cli, mtprototl.BadServerSaltID)
	bad := obj.(*mtprototl.BadServerSalt)
	if bad.BadMsgID != msgID || bad.BadMsgSeq != 5 || bad.ErrorCode != 48 || bad.NewSalt != h.salt {
		t.Fatalf("bad_server_salt = %+v, want id=%d seq=5 code=48 salt=%d", bad, msgID, h.salt)
	}
}

func TestEncryptedSessionReconnectContinuesWithSameAuthKeyAndSession(t *testing.T) {
	h := newHarness(t)
	lis, err := (&transport.TCPTransport{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	runServer(t, h.server, lis)

	base := client.NextMsgID()
	sessionInfo := client.SessionInfo{
		AuthKeyID: h.keyID, AuthKey: h.key, ServerSalt: h.salt, SessionID: h.session,
	}
	invoke := func(msgID int64, value int32) {
		t.Helper()
		conn, err := (&transport.TCPTransport{Protocol: transport.ProtocolIntermediate}).Dial(lis.Addr().String())
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		cli := client.New(conn,
			client.WithMsgIDGenerator(func() int64 { return msgID }),
			client.WithConstructors(map[uint32]func() tlrpc.TLObject{
				pingRespID: func() tlrpc.TLObject { return &pingResp{} },
			}),
		)
		cli.SetSessionInfo(sessionInfo)
		resp, err := cli.Invoke(context.Background(), &pingReq{Value: value})
		if err != nil {
			_ = cli.Close()
			t.Fatalf("invoke after dial: %v", err)
		}
		if got := resp.(*pingResp).Value; got != value+1 {
			t.Fatalf("response value = %d, want %d", got, value+1)
		}
		sessionInfo = cli.Session()
		if err := cli.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}

	invoke(base, 50)
	invoke(base+4, 60)
}

func TestNewSessionCreatedIsEmittedOncePerEstablishedSession(t *testing.T) {
	_, cli := dialEncryptedHarness(t)
	payload := serializeCompatObject(t, &pingReq{Value: 70})
	firstID := client.NextMsgID()
	writeEncryptedRequest(t, cli, firstID, 1, payload)
	first := drainRPCExchange(t, cli, firstID, true)
	created := first[mtprototl.NewSessionCreatedID].(*mtprototl.NewSessionCreated)
	if created.FirstMsgID != firstID || created.ServerSalt != cli.Session().ServerSalt {
		t.Fatalf("new_session_created = %+v, want first_msg_id=%d salt=%d", created, firstID, cli.Session().ServerSalt)
	}

	secondID := firstID + 4
	writeEncryptedRequest(t, cli, secondID, 3, payload)
	second := drainRPCExchange(t, cli, secondID, false)
	if _, duplicated := second[mtprototl.NewSessionCreatedID]; duplicated {
		t.Fatal("new_session_created was emitted more than once for one established session")
	}
}

func TestSameAuthKeyCanOwnIndependentMTProtoSessions(t *testing.T) {
	h, first := dialEncryptedHarness(t)
	payload := serializeCompatObject(t, &pingReq{Value: 80})
	firstID := client.NextMsgID()
	writeEncryptedRequest(t, first, firstID, 1, payload)
	drainRPCExchange(t, first, firstID, true)

	conn, err := (&transport.TCPTransport{Protocol: transport.ProtocolIntermediate}).Dial(h.listenerAddress)
	if err != nil {
		t.Fatalf("dial second session: %v", err)
	}
	defer func() { _ = conn.Close() }()
	second := client.New(conn, client.WithConstructors(map[uint32]func() tlrpc.TLObject{
		pingRespID: func() tlrpc.TLObject { return &pingResp{} },
	}))
	second.SetSession(h.keyID, h.key, h.salt, h.session+1)
	if _, err := second.Invoke(context.Background(), &pingReq{Value: 81}); err != nil {
		t.Fatalf("independent session on same auth key: %v", err)
	}
}

func TestMsgsAckIsConsumedWithoutAcknowledgingTheAcknowledgement(t *testing.T) {
	_, cli := dialEncryptedHarness(t)
	payload := serializeCompatObject(t, &mtprototl.MsgsAck{MsgIDs: []int64{41, 42}})
	writeEncryptedRequest(t, cli, client.NextMsgID(), 0, payload)
	if err := cli.Conn().SetReadDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	_, err := cli.Conn().ReadMessage(0)
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("msgs_ack produced a response: %v", err)
	}
}

func TestRPCDropAnswerUnknownTargetIsCorrelatedToControlRequest(t *testing.T) {
	_, cli := dialEncryptedHarness(t)
	requestMsgID := client.NextMsgID()
	payload := serializeCompatObject(t, &mtprototl.RPCDropAnswer{ReqMsgID: requestMsgID - 128})
	writeEncryptedRequest(t, cli, requestMsgID, 1, payload)

	_, obj := readUntilConstructor(t, cli, mtprototl.RPCResultID)
	result := obj.(*mtprototl.RPCResult)
	if result.ReqMsgID != requestMsgID {
		t.Fatalf("rpc_drop_answer result req_msg_id = %d, want %d", result.ReqMsgID, requestMsgID)
	}
	if len(result.ResultRaw) < 4 {
		t.Fatalf("rpc_drop_answer result is truncated: %d bytes", len(result.ResultRaw))
	}
	if got := mtprotoReadConstructor(result.ResultRaw); got != mtprototl.RPCAnswerUnknownID {
		t.Fatalf("rpc_drop_answer result constructor = 0x%08x, want rpc_answer_unknown", got)
	}
}

func TestServerStopCancelsActiveHandlerContextAndDrains(t *testing.T) {
	h := newHarness(t)
	handlerStarted := make(chan struct{})
	handlerCanceled := make(chan struct{})
	releaseHandler := make(chan struct{})
	h.ping.call = func(ctx context.Context, _ *pingReq) (*pingResp, error) {
		close(handlerStarted)
		select {
		case <-ctx.Done():
			close(handlerCanceled)
			return nil, ctx.Err()
		case <-releaseHandler:
			return nil, errors.New("test released handler after cancellation timeout")
		}
	}

	lis, err := (&transport.TCPTransport{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })
	go func() { _ = h.server.ServeTransport(lis) }()

	conn, err := (&transport.TCPTransport{Protocol: transport.ProtocolIntermediate}).Dial(lis.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	cli := client.New(conn, client.WithConstructors(map[uint32]func() tlrpc.TLObject{
		pingRespID: func() tlrpc.TLObject { return &pingResp{} },
	}))
	cli.SetSession(h.keyID, h.key, h.salt, h.session)

	invokeDone := make(chan error, 1)
	go func() {
		_, invokeErr := cli.Invoke(context.Background(), &pingReq{Value: 90})
		invokeDone <- invokeErr
	}()

	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}

	stopDone := make(chan error, 1)
	go func() { stopDone <- h.server.Stop() }()

	select {
	case <-handlerCanceled:
	case <-time.After(time.Second):
		close(releaseHandler)
		<-stopDone
		t.Fatal("server Stop did not cancel the active handler context")
	}

	select {
	case stopErr := <-stopDone:
		if stopErr != nil {
			t.Fatalf("stop server: %v", stopErr)
		}
	case <-time.After(time.Second):
		t.Fatal("server Stop did not drain after handler cancellation")
	}

	select {
	case invokeErr := <-invokeDone:
		if invokeErr == nil {
			t.Fatal("invoke unexpectedly succeeded after server shutdown")
		}
	case <-time.After(time.Second):
		t.Fatal("client invoke did not terminate after server shutdown")
	}
}

type encryptedHarness struct {
	*testHarness
	listenerAddress string
}

func dialEncryptedHarness(t *testing.T) (*encryptedHarness, *client.Client) {
	t.Helper()
	h := newHarness(t)
	lis, err := (&transport.TCPTransport{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	runServer(t, h.server, lis)
	conn, err := (&transport.TCPTransport{Protocol: transport.ProtocolIntermediate}).Dial(lis.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	cli := client.New(conn, client.WithConstructors(map[uint32]func() tlrpc.TLObject{
		pingRespID: func() tlrpc.TLObject { return &pingResp{} },
	}))
	cli.SetSession(h.keyID, h.key, h.salt, h.session)
	return &encryptedHarness{testHarness: h, listenerAddress: lis.Addr().String()}, cli
}

func serializeCompatObject(t *testing.T, obj tlrpc.TLObject) []byte {
	t.Helper()
	data, err := client.SerializeTL(obj)
	if err != nil {
		t.Fatalf("serialize %T: %v", obj, err)
	}
	return data
}

func writeEncryptedRequest(t *testing.T, cli *client.Client, msgID int64, seqNo int32, payload []byte) {
	t.Helper()
	packet, err := cli.EncryptMessage(msgID, seqNo, payload)
	if err != nil {
		t.Fatalf("encrypt request: %v", err)
	}
	if err := cli.Conn().WriteMessage(packet); err != nil {
		t.Fatalf("write request: %v", err)
	}
}

func drainRPCExchange(t *testing.T, cli *client.Client, reqMsgID int64, wantNewSession bool) map[uint32]tlrpc.TLObject {
	t.Helper()
	want := map[uint32]bool{
		mtprototl.RPCResultID: true,
		mtprototl.MsgsAckID:   true,
	}
	if wantNewSession {
		want[mtprototl.NewSessionCreatedID] = true
	}
	seen := make(map[uint32]tlrpc.TLObject, len(want))
	for len(seen) < len(want) {
		_, obj := readKnownObject(t, cli)
		id := obj.ConstructorID()
		if id == mtprototl.RPCResultID && obj.(*mtprototl.RPCResult).ReqMsgID != reqMsgID {
			continue
		}
		if want[id] {
			seen[id] = obj
		}
	}
	return seen
}

func readUntilConstructor(t *testing.T, cli *client.Client, constructorID uint32) (*mtproto.InnerData, tlrpc.TLObject) {
	t.Helper()
	for i := 0; i < 8; i++ {
		inner, obj := readKnownObject(t, cli)
		if obj.ConstructorID() == constructorID {
			return inner, obj
		}
	}
	t.Fatalf("constructor 0x%08x not received", constructorID)
	return nil, nil
}

func readKnownObject(t *testing.T, cli *client.Client) (*mtproto.InnerData, tlrpc.TLObject) {
	t.Helper()
	if err := cli.Conn().SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	packet, err := cli.Conn().ReadMessage(0)
	if err != nil {
		t.Fatalf("read encrypted response: %v", err)
	}
	inner, err := cli.DecryptMessage(packet)
	if err != nil {
		t.Fatalf("decrypt response: %v", err)
	}
	obj, err := decodeConformanceObject(inner.Data)
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return inner, obj
}

func decodeConformanceObject(data []byte) (tlrpc.TLObject, error) {
	if len(data) < 4 {
		return nil, io.ErrUnexpectedEOF
	}
	var obj tlrpc.TLObject
	switch mtprotoReadConstructor(data) {
	case mtprototl.BadMsgNotificationID:
		obj = &mtprototl.BadMsgNotification{}
	case mtprototl.BadServerSaltID:
		obj = &mtprototl.BadServerSalt{}
	case mtprototl.NewSessionCreatedID:
		obj = &mtprototl.NewSessionCreated{}
	case mtprototl.MsgsAckID:
		obj = &mtprototl.MsgsAck{}
	case mtprototl.RPCResultID:
		obj = &mtprototl.RPCResult{}
	default:
		return nil, fmt.Errorf("unknown conformance constructor 0x%08x", mtprotoReadConstructor(data))
	}
	if err := obj.(interface{ DeserializeTL(io.Reader) error }).DeserializeTL(bytes.NewReader(data)); err != nil {
		return nil, err
	}
	return obj, nil
}

func mtprotoReadConstructor(data []byte) uint32 {
	return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
}
