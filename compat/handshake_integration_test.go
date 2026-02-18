package compat

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/compat/client"
	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/examples/gen"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
)

func newHandshakeHarness(t *testing.T) (*tlrpc.Server, *crypto.ServerKey) {
	t.Helper()
	memAuth := crypto.NewMemoryAuthKeyManager()
	serverKeys := crypto.NewMemoryServerKeyManager()
	serverKey, err := crypto.GenerateServerKey()
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	serverKeys.AddKey(serverKey)
	sessions := session.NewMemoryManager()

	srv := tlrpc.NewServer(
		tlrpc.WithAuthKeyManager(memAuth),
		tlrpc.WithSessionManager(sessions),
		tlrpc.WithServerKeyManager(serverKeys),
		tlrpc.WithMaxLayer(170),
	)
	srv.RegisterConstructor((&gen.HelpGetConfigRequest{}).ConstructorID(), func() tlrpc.TLObject {
		return &gen.HelpGetConfigRequest{}
	})
	srv.RegisterMethod((&gen.HelpGetConfigRequest{}).ConstructorID(), func(ctx context.Context, obj tlrpc.TLObject) (interface{}, error) {
		_ = obj.(*gen.HelpGetConfigRequest)
		now := int32(time.Now().Unix())
		return &gen.Config{
			Date:                    now,
			Expires:                 now + 3600,
			ThisDc:                  1,
			DcTxtDomainName:         "localhost",
			ChatSizeMax:             200,
			MegagroupSizeMax:        10000,
			ForwardedCountMax:       100,
			OnlineUpdatePeriodMs:    30000,
			OfflineBlurTimeoutMs:    5000,
			OfflineIdleTimeoutMs:    30000,
			OnlineCloudTimeoutMs:    30000,
			NotifyCloudDelayMs:      3000,
			NotifyDefaultDelayMs:    1500,
			PushChatPeriodMs:        30000,
			PushChatLimit:           2,
			EditTimeLimit:           172800,
			RevokeTimeLimit:         172800,
			RevokePmTimeLimit:       172800,
			RatingEDecay:            2419200,
			StickersRecentLimit:     30,
			ChannelsReadMediaPeriod: 604800,
			CallReceiveTimeoutMs:    20000,
			CallRingTimeoutMs:       90000,
			CallConnectTimeoutMs:    30000,
			CallPacketTimeoutMs:     10000,
			MeURLPrefix:             "https://t.me/",
			CaptionLengthMax:        1024,
			MessageLengthMax:        4096,
			WebfileDcID:             1,
		}, nil
	})
	return srv, serverKey
}

func runServer(t *testing.T, srv *tlrpc.Server, lis transport.Listener) {
	t.Helper()
	t.Cleanup(func() {
		_ = srv.Stop()
		_ = lis.Close()
	})
	go func() {
		_ = srv.ServeTransport(lis)
	}()
}

func dialClientTCP(t *testing.T, addr string, codec client.Codec, serverKey *crypto.ServerKey) *client.Client {
	t.Helper()
	c, err := client.DialTCP(addr, codec,
		client.WithServerKey(serverKey),
		client.WithConstructors(gen.GetStaticConstructors()),
	)
	if err != nil {
		t.Fatalf("tcp dial: %v", err)
	}
	return c
}

func dialClientWS(t *testing.T, addr string, serverKey *crypto.ServerKey) *client.Client {
	t.Helper()
	c, err := client.DialWS(addr,
		client.WithServerKey(serverKey),
		client.WithConstructors(gen.GetStaticConstructors()),
	)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	return c
}

func TestMTProtoHandshakeAcrossTCPTransports(t *testing.T) {
	for _, tc := range []struct {
		name  string
		proto transport.Protocol
		codec client.Codec
	}{
		{"abridged", transport.ProtocolAbridged, client.CodecAbridged},
		{"intermediate", transport.ProtocolIntermediate, client.CodecIntermediate},
		{"padded", transport.ProtocolPaddedIntermediate, client.CodecPadded},
		{"full", transport.ProtocolFull, client.CodecFull},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, serverKey := newHandshakeHarness(t)
			lis, err := (&transport.TCPTransport{Protocol: tc.proto}).Listen("127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			runServer(t, srv, lis)

			cli := dialClientTCP(t, lis.Addr().String(), tc.codec, serverKey)
			defer func() { _ = cli.Close() }()

			if _, err := cli.Handshake(context.Background()); err != nil {
				t.Fatalf("handshake: %v", err)
			}
			resp, err := cli.InvokeWrapped(context.Background(), 170, client.InitParams{
				APIID:          77777,
				DeviceModel:    "compat-test",
				SystemVersion:  "test",
				AppVersion:     "1.0",
				SystemLangCode: "en",
				LangPack:       "",
				LangCode:       "en",
			}, &gen.HelpGetConfigRequest{}, false)
			if err != nil {
				t.Fatalf("invoke wrapped: %v", err)
			}
			if _, ok := resp.(*gen.Config); !ok {
				t.Fatalf("unexpected response type %T", resp)
			}
		})
	}
}

func TestMTProtoHandshakeWebSocketObfuscated2Padded(t *testing.T) {
	srv, serverKey := newHandshakeHarness(t)
	lis, err := (&transport.WebSocketTransport{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	runServer(t, srv, lis)
	addr := lis.Addr().(*net.TCPAddr)

	cli := dialClientWS(t, fmt.Sprintf("ws://%s:%d", addr.IP.String(), addr.Port), serverKey)
	defer func() { _ = cli.Close() }()

	if _, err := cli.Handshake(context.Background()); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	resp, err := cli.InvokeWrapped(context.Background(), 170, client.InitParams{
		APIID:          77777,
		DeviceModel:    "compat-test",
		SystemVersion:  "test",
		AppVersion:     "1.0",
		SystemLangCode: "en",
		LangPack:       "",
		LangCode:       "en",
	}, &gen.HelpGetConfigRequest{}, false)
	if err != nil {
		t.Fatalf("invoke wrapped: %v", err)
	}
	if _, ok := resp.(*gen.Config); !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
}

func TestHandshakeNegativeWrongMsgIDGetsBadMsgNotification(t *testing.T) {
	srv, serverKey := newHandshakeHarness(t)
	lis, err := (&transport.TCPTransport{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	runServer(t, srv, lis)

	cli := dialClientTCP(t, lis.Addr().String(), client.CodecIntermediate, serverKey)
	defer func() { _ = cli.Close() }()
	if _, err := cli.Handshake(context.Background()); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if _, err := cli.InvokeWrapped(context.Background(), 170, client.InitParams{APIID: 1, DeviceModel: "test", SystemVersion: "test", AppVersion: "1.0", SystemLangCode: "en", LangCode: "en"}, &gen.HelpGetConfigRequest{}, false); err != nil {
		t.Fatalf("prime invoke: %v", err)
	}

	payload, err := client.SerializeTL(&gen.HelpGetConfigRequest{})
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	msgID1 := client.NextMsgID()
	packet, err := cli.EncryptMessage(msgID1, 1, payload)
	if err != nil {
		t.Fatalf("encrypt request: %v", err)
	}
	if err := cli.Conn().WriteMessage(packet); err != nil {
		t.Fatalf("write request: %v", err)
	}
	_, _ = cli.Conn().ReadMessage()

	packet, err = cli.EncryptMessage(msgID1-4, 1, payload)
	if err != nil {
		t.Fatalf("encrypt request: %v", err)
	}
	if err := cli.Conn().WriteMessage(packet); err != nil {
		t.Fatalf("write request: %v", err)
	}

	for i := 0; i < 4; i++ {
		packet, err := cli.Conn().ReadMessage()
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		dec, err := cli.DecryptMessage(packet)
		if err != nil {
			continue
		}
		ctor := binary.LittleEndian.Uint32(dec.Data[:4])
		if ctor == mtprototl.BadMsgNotificationID {
			return
		}
	}
	t.Fatalf("bad_msg_notification not received")
}

func TestHandshakeNegativeWrongSaltGetsBadServerSalt(t *testing.T) {
	srv, serverKey := newHandshakeHarness(t)
	lis, err := (&transport.TCPTransport{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	runServer(t, srv, lis)

	cli := dialClientTCP(t, lis.Addr().String(), client.CodecIntermediate, serverKey)
	defer func() { _ = cli.Close() }()
	if _, err := cli.Handshake(context.Background()); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if _, err := cli.InvokeWrapped(context.Background(), 170, client.InitParams{APIID: 1, DeviceModel: "test", SystemVersion: "test", AppVersion: "1.0", SystemLangCode: "en", LangCode: "en"}, &gen.HelpGetConfigRequest{}, false); err != nil {
		t.Fatalf("prime invoke: %v", err)
	}

	payload, err := client.SerializeTL(&gen.HelpGetConfigRequest{})
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	state := cli.Session()
	packet, err := cli.EncryptMessageWithSalt(state.ServerSalt+1, client.NextMsgID(), 1, payload)
	if err != nil {
		t.Fatalf("encrypt request: %v", err)
	}
	if err := cli.Conn().WriteMessage(packet); err != nil {
		t.Fatalf("write request: %v", err)
	}

	for i := 0; i < 4; i++ {
		packet, err := cli.Conn().ReadMessage()
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		dec, err := cli.DecryptMessage(packet)
		if err != nil {
			continue
		}
		ctor := binary.LittleEndian.Uint32(dec.Data[:4])
		if ctor == mtprototl.BadServerSaltID {
			return
		}
	}
	t.Fatalf("bad_server_salt not received")
}

func TestHandshakeNegativeInvalidEncryptedDataFailsCleanly(t *testing.T) {
	srv, _ := newHandshakeHarness(t)
	lis, err := (&transport.TCPTransport{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	runServer(t, srv, lis)

	conn, err := (&transport.TCPTransport{Protocol: transport.ProtocolIntermediate}).Dial(lis.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// auth_key_id + msg_key + non-block-size encrypted payload -> decrypt must fail cleanly.
	raw := make([]byte, 8+16+7)
	binary.LittleEndian.PutUint64(raw[:8], uint64(1))
	copy(raw[8:24], bytes.Repeat([]byte{0x42}, 16))
	copy(raw[24:], []byte{1, 2, 3, 4, 5, 6, 7})
	if err := conn.WriteMessage(raw); err != nil {
		t.Fatalf("write malformed encrypted packet: %v", err)
	}
	_ = conn.Close()

	// Server should continue serving new connections after malformed input.
	conn2, err := (&transport.TCPTransport{Protocol: transport.ProtocolIntermediate}).Dial(lis.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn2.Close() }()
}
