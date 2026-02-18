package compat

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/compat/client"
	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
)

const (
	pingReqID  uint32 = 0x10203040
	pingRespID uint32 = 0x11223344
)

type pingReq struct {
	Value int32
}

func (*pingReq) ConstructorID() uint32 { return pingReqID }

func (r *pingReq) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, r.ConstructorID()); err != nil {
		return err
	}
	return mtproto.WriteInt32(w, r.Value)
}

func (r *pingReq) DeserializeTL(rd io.Reader) error {
	ctor, err := mtproto.ReadUint32(rd)
	if err != nil {
		return err
	}
	if ctor != r.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x", ctor)
	}
	r.Value, err = mtproto.ReadInt32(rd)
	return err
}

type pingResp struct {
	Value int32
}

func (*pingResp) ConstructorID() uint32 { return pingRespID }

func (r *pingResp) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, r.ConstructorID()); err != nil {
		return err
	}
	return mtproto.WriteInt32(w, r.Value)
}

func (r *pingResp) DeserializeTL(rd io.Reader) error {
	ctor, err := mtproto.ReadUint32(rd)
	if err != nil {
		return err
	}
	if ctor != r.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x", ctor)
	}
	r.Value, err = mtproto.ReadInt32(rd)
	return err
}

type testHarness struct {
	server   *tlrpc.Server
	authKeys crypto.AuthKeyManager
	sessions session.Manager
	keyID    crypto.KeyID
	key      crypto.AuthKey
	salt     int64
	session  int64
}

func newHarness(t *testing.T) *testHarness {
	t.Helper()
	authKeys := crypto.NewMemoryAuthKeyManager()
	sessions := session.NewMemoryManager()
	srv := tlrpc.NewServer(
		tlrpc.WithAuthKeyManager(authKeys),
		tlrpc.WithSessionManager(sessions),
		tlrpc.WithMaxLayer(170),
	)
	keyID := crypto.KeyID(0x0102030405060708)
	var key crypto.AuthKey
	for i := range key {
		key[i] = byte(i)
	}
	if err := authKeys.Put(keyID, key); err != nil {
		t.Fatalf("put auth key: %v", err)
	}
	sess, err := sessions.Create(keyID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess.ServerSalt = 0x1122334455667788
	sess.SessionID = 0x0101010102020202
	if err := sessions.Save(sess); err != nil {
		t.Fatalf("save session: %v", err)
	}

	srv.RegisterConstructor(pingReqID, func() tlrpc.TLObject { return &pingReq{} })
	srv.RegisterMethod(pingReqID, func(ctx context.Context, obj tlrpc.TLObject) (interface{}, error) {
		req := obj.(*pingReq)
		return &pingResp{Value: req.Value + 1}, nil
	})

	return &testHarness{
		server:   srv,
		authKeys: authKeys,
		sessions: sessions,
		keyID:    keyID,
		key:      key,
		salt:     sess.ServerSalt,
		session:  sess.SessionID,
	}
}

func TestTCPTransportsEncryptedRPC(t *testing.T) {
	for _, proto := range []transport.Protocol{
		transport.ProtocolAbridged,
		transport.ProtocolIntermediate,
		transport.ProtocolPaddedIntermediate,
		transport.ProtocolFull,
	} {
		t.Run(fmt.Sprintf("proto_%d", proto), func(t *testing.T) {
			h := newHarness(t)
			tcp := &transport.TCPTransport{}
			lis, err := tcp.Listen("127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			runServer(t, h.server, lis)

			cli, err := (&transport.TCPTransport{Protocol: proto}).Dial(lis.Addr().String())
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			t.Cleanup(func() { _ = cli.Close() })

			c := client.New(cli, client.WithConstructors(map[uint32]func() tlrpc.TLObject{
				pingRespID: func() tlrpc.TLObject { return &pingResp{} },
			}))
			c.SetSession(h.keyID, h.key, h.salt, h.session)
			respObj, err := c.Invoke(context.Background(), &pingReq{Value: 10})
			if err != nil {
				t.Fatalf("invoke: %v", err)
			}
			resp := respObj.(*pingResp)
			if resp.Value != 11 {
				t.Fatalf("unexpected response value: %d", resp.Value)
			}
		})
	}
}

func TestWebSocketObfuscatedPaddedIntermediate(t *testing.T) {
	h := newHarness(t)
	ws := &transport.WebSocketTransport{}
	lis, err := ws.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	runServer(t, h.server, lis)

	addr := lis.Addr().(*net.TCPAddr)
	cli, err := (&transport.WebSocketTransport{Protocol: transport.ProtocolPaddedIntermediate}).Dial(
		fmt.Sprintf("ws://%s:%d", addr.IP.String(), addr.Port),
	)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	c := client.New(cli, client.WithConstructors(map[uint32]func() tlrpc.TLObject{
		pingRespID: func() tlrpc.TLObject { return &pingResp{} },
	}))
	c.SetSession(h.keyID, h.key, h.salt, h.session)
	respObj, err := c.Invoke(context.Background(), &pingReq{Value: 22})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	resp := respObj.(*pingResp)
	if resp.Value != 23 {
		t.Fatalf("unexpected response value: %d", resp.Value)
	}
}

func TestWrappedInvokeWithLayerInitConnection(t *testing.T) {
	h := newHarness(t)

	seenLayer := 0
	seenAPIID := int32(0)
	h.server.RegisterMethod(pingReqID, func(ctx context.Context, obj tlrpc.TLObject) (interface{}, error) {
		seenLayer = tlrpc.LayerFromContext(ctx)
		if sess := tlrpc.SessionFromContext(ctx); sess != nil {
			if apiIDValue, ok := sess.Data.Load("init.api_id"); ok {
				seenAPIID, _ = apiIDValue.(int32)
			}
		}
		req := obj.(*pingReq)
		return &pingResp{Value: req.Value + 1}, nil
	})

	tcp := &transport.TCPTransport{}
	lis, err := tcp.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	runServer(t, h.server, lis)

	cli, err := (&transport.TCPTransport{Protocol: transport.ProtocolIntermediate}).Dial(lis.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	c := client.New(cli, client.WithConstructors(map[uint32]func() tlrpc.TLObject{
		pingRespID: func() tlrpc.TLObject { return &pingResp{} },
	}))
	c.SetSession(h.keyID, h.key, h.salt, h.session)
	respObj, err := c.InvokeWrapped(context.Background(), 150, client.InitParams{
		APIID:          1000,
		DeviceModel:    "test-device",
		SystemVersion:  "1.0",
		AppVersion:     "1.0",
		SystemLangCode: "en",
		LangPack:       "",
		LangCode:       "en",
	}, &pingReq{Value: 7}, false)
	if err != nil {
		t.Fatalf("invoke wrapped: %v", err)
	}
	resp := respObj.(*pingResp)
	if resp.Value != 8 {
		t.Fatalf("unexpected response value: %d", resp.Value)
	}
	if seenLayer != 150 {
		t.Fatalf("handler did not receive wrapper layer: got %d", seenLayer)
	}
	if seenAPIID != 1000 {
		t.Fatalf("handler did not receive initConnection side effect api_id: got %d", seenAPIID)
	}
}
