package compat

import (
	"bytes"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/compat/client"
	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/transport"
)

// TestRuntimeReliabilityConformance exercises reliability exclusively through
// an encrypted public transport connection. In MTProto direction terms,
// msgs_state_req describes messages sent by the requester (server-inbound),
// while msg_resend_req and msgs_ack address messages sent by the server.
func TestRuntimeReliabilityConformance(t *testing.T) {
	t.Run("state reports a completed request and an unknown ID", func(t *testing.T) {
		_, cli := dialEncryptedHarness(t)
		ids := newReliabilityMessageIDs()
		requestID := ids.next()
		response := sendReliabilityRPC(t, cli, requestID, 101)

		stateRequestID := ids.next()
		writeEncryptedRequest(t, cli, stateRequestID, 2, serializeCompatObject(t, &mtprototl.MsgsStateReq{
			MsgIDs: []int64{requestID, requestID - 4},
		}))

		_, _, object := readReliabilityPacket(t, cli)
		info, ok := object.(*mtprototl.MsgsStateInfo)
		if !ok {
			t.Fatalf("response constructor = 0x%08x, want msgs_state_info", object.ConstructorID())
		}
		wantKnown := byte(mtprototl.MessageStateReceived |
			mtprototl.MessageStateAcknowledged |
			mtprototl.MessageStateProcessing |
			mtprototl.MessageStateResponseGenerated |
			mtprototl.MessageStateKnown)
		want := []byte{wantKnown, mtprototl.MessageStateUnknownTooOld}
		if info.ReqMsgID != stateRequestID || !bytes.Equal(info.Info, want) {
			t.Fatalf("msgs_state_info = req:%d info:%v, want req:%d info:%v", info.ReqMsgID, info.Info, stateRequestID, want)
		}
		if response.inner.MsgID == requestID {
			t.Fatal("test setup did not distinguish server-outbound and server-inbound message IDs")
		}
	})

	t.Run("resend reproduces the exact retained encrypted packet", func(t *testing.T) {
		_, cli := dialEncryptedHarness(t)
		ids := newReliabilityMessageIDs()
		response := sendReliabilityRPC(t, cli, ids.next(), 202)

		resendRequestID := ids.next()
		writeEncryptedRequest(t, cli, resendRequestID, 2, serializeCompatObject(t, &mtprototl.MsgResendReq{
			MsgIDs: []int64{response.inner.MsgID},
		}))
		resentPacket, resentInner, object := readReliabilityPacket(t, cli)
		if !bytes.Equal(resentPacket, response.packet) {
			t.Fatal("msg_resend_req did not reproduce the exact retained encrypted packet")
		}
		if resentInner.MsgID != response.inner.MsgID ||
			resentInner.SeqNo != response.inner.SeqNo ||
			!bytes.Equal(resentInner.Data, response.inner.Data) {
			t.Fatalf("resent inner message = id:%d seq:%d data:%x, want id:%d seq:%d data:%x",
				resentInner.MsgID, resentInner.SeqNo, resentInner.Data,
				response.inner.MsgID, response.inner.SeqNo, response.inner.Data)
		}
		if result, ok := object.(*mtprototl.RPCResult); !ok || result.ReqMsgID != response.result.ReqMsgID {
			t.Fatalf("resent object = %#v, want rpc_result correlated to %d", object, response.result.ReqMsgID)
		}
	})

	t.Run("acknowledgement makes a retained response ineligible for resend", func(t *testing.T) {
		_, cli := dialEncryptedHarness(t)
		ids := newReliabilityMessageIDs()
		response := sendReliabilityRPC(t, cli, ids.next(), 303)

		writeEncryptedRequest(t, cli, ids.next(), 2, serializeCompatObject(t, &mtprototl.MsgsAck{
			MsgIDs: []int64{response.inner.MsgID},
		}))
		resendRequestID := ids.next()
		writeEncryptedRequest(t, cli, resendRequestID, 2, serializeCompatObject(t, &mtprototl.MsgResendReq{
			MsgIDs: []int64{response.inner.MsgID},
		}))

		packet, _, object := readReliabilityPacket(t, cli)
		if bytes.Equal(packet, response.packet) {
			t.Fatal("acknowledged response was retransmitted")
		}
		info, ok := object.(*mtprototl.MsgsStateInfo)
		if !ok {
			t.Fatalf("post-ack resend response constructor = 0x%08x, want msgs_state_info", object.ConstructorID())
		}
		want := []byte{mtprototl.MessageStateReceived | mtprototl.MessageStateAcknowledged | mtprototl.MessageStateKnown}
		if info.ReqMsgID != resendRequestID || !bytes.Equal(info.Info, want) {
			t.Fatalf("post-ack msgs_state_info = req:%d info:%v, want req:%d info:%v", info.ReqMsgID, info.Info, resendRequestID, want)
		}
	})

	t.Run("unacknowledged response survives reconnect", func(t *testing.T) {
		h, first := dialEncryptedHarness(t)
		ids := newReliabilityMessageIDs()
		response := sendReliabilityRPC(t, first, ids.next(), 404)
		if err := first.Close(); err != nil {
			t.Fatalf("close first connection: %v", err)
		}

		conn, err := (&transport.TCPTransport{Protocol: transport.ProtocolIntermediate}).Dial(h.listenerAddress)
		if err != nil {
			t.Fatalf("dial reconnect: %v", err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		reconnected := client.New(conn, client.WithConstructors(map[uint32]func() tlrpc.TLObject{
			pingRespID: func() tlrpc.TLObject { return &pingResp{} },
		}))
		reconnected.SetSession(h.keyID, h.key, h.salt, h.session)

		writeEncryptedRequest(t, reconnected, ids.next(), 2, serializeCompatObject(t, &mtprototl.MsgResendReq{
			MsgIDs: []int64{response.inner.MsgID},
		}))
		resentPacket, resentInner, object := readReliabilityPacket(t, reconnected)
		if !bytes.Equal(resentPacket, response.packet) {
			t.Fatal("reconnect did not retain the exact unacknowledged encrypted packet")
		}
		if resentInner.MsgID != response.inner.MsgID || !bytes.Equal(resentInner.Data, response.inner.Data) {
			t.Fatalf("reconnected resend inner message differs: got id:%d data:%x, want id:%d data:%x",
				resentInner.MsgID, resentInner.Data, response.inner.MsgID, response.inner.Data)
		}
		if result, ok := object.(*mtprototl.RPCResult); !ok || result.ReqMsgID != response.result.ReqMsgID {
			t.Fatalf("reconnected resend object = %#v, want rpc_result correlated to %d", object, response.result.ReqMsgID)
		}
	})
}

type reliabilityMessageIDs struct {
	current int64
}

func newReliabilityMessageIDs() *reliabilityMessageIDs {
	return &reliabilityMessageIDs{current: client.NextMsgID() - 4}
}

func (g *reliabilityMessageIDs) next() int64 {
	g.current += 4
	return g.current
}

type retainedRPCResponse struct {
	packet []byte
	inner  *mtproto.InnerData
	result *mtprototl.RPCResult
}

func sendReliabilityRPC(t *testing.T, cli *client.Client, requestID int64, value int32) retainedRPCResponse {
	t.Helper()
	writeEncryptedRequest(t, cli, requestID, 1, serializeCompatObject(t, &pingReq{Value: value}))

	var response retainedRPCResponse
	seen := make(map[uint32]bool, 3)
	for len(seen) < 3 {
		packet, inner, object := readReliabilityPacket(t, cli)
		switch value := object.(type) {
		case *mtprototl.RPCResult:
			if value.ReqMsgID != requestID {
				continue
			}
			response = retainedRPCResponse{
				packet: append([]byte(nil), packet...),
				inner:  cloneReliabilityInner(inner),
				result: value,
			}
		case *mtprototl.MsgsAck:
		case *mtprototl.NewSessionCreated:
		default:
			t.Fatalf("unexpected initial exchange object %T", object)
		}
		seen[object.ConstructorID()] = true
	}
	if response.inner == nil {
		t.Fatal("initial exchange did not produce a correlated rpc_result")
	}
	return response
}

func readReliabilityPacket(t *testing.T, cli *client.Client) ([]byte, *mtproto.InnerData, tlrpc.TLObject) {
	t.Helper()
	if err := cli.Conn().SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set reliability read deadline: %v", err)
	}
	packet, err := cli.Conn().ReadMessage(0)
	if err != nil {
		t.Fatalf("read encrypted reliability packet: %v", err)
	}
	inner, err := cli.DecryptMessage(packet)
	if err != nil {
		t.Fatalf("decrypt reliability packet: %v", err)
	}
	object, err := decodeReliabilityObject(inner.Data)
	if err != nil {
		t.Fatalf("decode reliability packet: %v", err)
	}
	return packet, inner, object
}

func decodeReliabilityObject(data []byte) (tlrpc.TLObject, error) {
	if len(data) < 4 {
		return nil, io.ErrUnexpectedEOF
	}
	if mtprotoReadConstructor(data) != mtprototl.MsgsStateInfoID {
		return decodeConformanceObject(data)
	}
	object := &mtprototl.MsgsStateInfo{}
	if err := object.DeserializeTL(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("decode msgs_state_info: %w", err)
	}
	return object, nil
}

func cloneReliabilityInner(inner *mtproto.InnerData) *mtproto.InnerData {
	clone := *inner
	clone.Data = append([]byte(nil), inner.Data...)
	return &clone
}
