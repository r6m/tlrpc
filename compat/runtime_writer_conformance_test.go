package compat

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/compat/client"
	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/transport"
)

const runtimeWriterPushID uint32 = 0x7f00b002

type runtimeWriterPush struct {
	Value int32
}

func (*runtimeWriterPush) ConstructorID() uint32 { return runtimeWriterPushID }

func (p *runtimeWriterPush) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, p.ConstructorID()); err != nil {
		return err
	}
	return mtproto.WriteInt32(w, p.Value)
}

func (p *runtimeWriterPush) DeserializeTL(r io.Reader) error {
	constructorID, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if constructorID != p.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x", constructorID)
	}
	p.Value, err = mtproto.ReadInt32(r)
	return err
}

func TestRuntimeWriterMixedOutboundEncryptedWireConformance(t *testing.T) {
	const userID int64 = 7001

	h := newHarness(t)
	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseHandler) }) })

	h.ping.call = func(ctx context.Context, req *pingReq) (*pingResp, error) {
		switch req.Value {
		case 1:
			if err := tlrpc.BindSessionUser(ctx, userID); err != nil {
				return nil, err
			}
		case 2:
			close(handlerStarted)
			select {
			case <-releaseHandler:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return &pingResp{Value: req.Value + 1}, nil
	}

	_, cli := dialEncryptedHarnessFromHarness(t, h)
	firstRequestID := client.NextMsgID()
	writeEncryptedRequest(t, cli, firstRequestID, 1, serializeCompatObject(t, &pingReq{Value: 1}))
	drainRPCExchange(t, cli, firstRequestID, true)

	mixedRequestID := firstRequestID + 4
	writeEncryptedRequest(t, cli, mixedRequestID, 3, serializeCompatObject(t, &pingReq{Value: 2}))

	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("mixed RPC handler did not start")
	}

	publishDone := make(chan error, 1)
	go func() {
		publishDone <- h.server.Publish(userID, &runtimeWriterPush{Value: 99})
	}()
	select {
	case err := <-publishDone:
		if err != nil {
			t.Fatalf("publish while RPC handler was active: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("publish did not complete while RPC handler was active")
	}

	releaseOnce.Do(func() { close(releaseHandler) })

	wantConstructors := []uint32{
		runtimeWriterPushID,
		mtprototl.RPCResultID,
		mtprototl.MsgsAckID,
	}
	var (
		lastMessageID   int64
		lastContentSeq  int32 = -1
		rpcResultCounts       = make(map[int64]int)
		pushCount       int
		acknowledged    bool
	)
	for wireIndex, wantConstructor := range wantConstructors {
		inner, obj := readRuntimeWriterWireObject(t, cli)
		if got := obj.ConstructorID(); got != wantConstructor {
			t.Fatalf("wire message %d constructor = 0x%08x, want 0x%08x", wireIndex, got, wantConstructor)
		}
		if wireIndex > 0 && inner.MsgID <= lastMessageID {
			t.Fatalf("wire message IDs are not strictly increasing: %d then %d", lastMessageID, inner.MsgID)
		}
		lastMessageID = inner.MsgID

		contentRelated := obj.ConstructorID() != mtprototl.MsgsAckID
		if contentRelated {
			if inner.SeqNo&1 == 0 {
				t.Fatalf("content-related wire message %d has even sequence number %d", wireIndex, inner.SeqNo)
			}
			if lastContentSeq >= 0 && inner.SeqNo != lastContentSeq+2 {
				t.Fatalf("content-related sequence numbers do not advance in wire order: %d then %d", lastContentSeq, inner.SeqNo)
			}
			lastContentSeq = inner.SeqNo
		} else {
			if inner.SeqNo&1 != 0 {
				t.Fatalf("non-content wire message %d has odd sequence number %d", wireIndex, inner.SeqNo)
			}
			if lastContentSeq >= 0 && inner.SeqNo != lastContentSeq+1 {
				t.Fatalf("non-content sequence %d does not reflect preceding content sequence %d", inner.SeqNo, lastContentSeq)
			}
		}

		switch value := obj.(type) {
		case *runtimeWriterPush:
			pushCount++
			if inner.MsgID&3 != 3 {
				t.Fatalf("push msg_id low bits = %d, want canonical server-push value 3", inner.MsgID&3)
			}
			if value.Value != 99 {
				t.Fatalf("push value = %d, want 99", value.Value)
			}
		case *mtprototl.RPCResult:
			if inner.MsgID&3 != 1 {
				t.Fatalf("RPC result msg_id low bits = %d, want canonical response value 1", inner.MsgID&3)
			}
			rpcResultCounts[value.ReqMsgID]++
			response := &pingResp{}
			if err := response.DeserializeTL(bytes.NewReader(value.ResultRaw)); err != nil {
				t.Fatalf("decode RPC result body: %v", err)
			}
			if response.Value != 3 {
				t.Fatalf("RPC result value = %d, want 3", response.Value)
			}
		case *mtprototl.MsgsAck:
			if inner.MsgID&3 != 1 {
				t.Fatalf("ACK msg_id low bits = %d, want canonical response value 1", inner.MsgID&3)
			}
			for _, messageID := range value.MsgIDs {
				if messageID == mixedRequestID {
					acknowledged = true
				}
			}
		}
	}

	if pushCount != 1 {
		t.Fatalf("top-level pushes = %d, want exactly 1 (push must not be wrapped as rpc_result)", pushCount)
	}
	if rpcResultCounts[mixedRequestID] != 1 || len(rpcResultCounts) != 1 {
		t.Fatalf("RPC result correlation counts = %v, want exactly one result for request %d", rpcResultCounts, mixedRequestID)
	}
	if !acknowledged {
		t.Fatalf("protocol ACK does not contain mixed request ID %d", mixedRequestID)
	}
}

func dialEncryptedHarnessFromHarness(t *testing.T, h *testHarness) (*encryptedHarness, *client.Client) {
	t.Helper()
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

func readRuntimeWriterWireObject(t *testing.T, cli *client.Client) (*mtproto.InnerData, tlrpc.TLObject) {
	t.Helper()
	if err := cli.Conn().SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	packet, err := cli.Conn().ReadMessage(0)
	if err != nil {
		t.Fatalf("read encrypted wire message: %v", err)
	}
	inner, err := cli.DecryptMessage(packet)
	if err != nil {
		t.Fatalf("decrypt wire message: %v", err)
	}

	var obj tlrpc.TLObject
	switch constructorID := mtprotoReadConstructor(inner.Data); constructorID {
	case runtimeWriterPushID:
		obj = &runtimeWriterPush{}
	case mtprototl.RPCResultID:
		obj = &mtprototl.RPCResult{}
	case mtprototl.MsgsAckID:
		obj = &mtprototl.MsgsAck{}
	default:
		t.Fatalf("unexpected wire constructor 0x%08x", constructorID)
	}
	if err := obj.(interface{ DeserializeTL(io.Reader) error }).DeserializeTL(bytes.NewReader(inner.Data)); err != nil {
		t.Fatalf("decode wire object: %v", err)
	}
	return inner, obj
}
