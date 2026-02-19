package compat

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/compat/client"
	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/examples/gen"
	"github.com/r6m/tlrpc/internal/compatkeys"
	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
)

const compatPushID uint32 = 0x7f00b001

type compatPush struct {
	Value int32
}

func (p *compatPush) ConstructorID() uint32 { return compatPushID }
func (p *compatPush) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, p.ConstructorID()); err != nil {
		return err
	}
	return mtproto.WriteInt32(w, p.Value)
}
func (p *compatPush) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != p.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %x want %x", ctor, p.ConstructorID())
	}
	val, err := mtproto.ReadInt32(r)
	if err != nil {
		return err
	}
	p.Value = val
	return nil
}

func TestCompatMinimalRPCAndPush(t *testing.T) {
	compatKey, err := compatkeys.ServerKey()
	if err != nil {
		t.Fatalf("load compat key: %v", err)
	}
	serverKeys := crypto.NewMemoryServerKeyManager()
	serverKeys.AddKey(compatKey)

	authKeys := crypto.NewMemoryAuthKeyManager()
	sessions := session.NewMemoryManager()

	srv := tlrpc.NewServer(
		tlrpc.WithAuthKeyManager(authKeys),
		tlrpc.WithSessionManager(sessions),
		tlrpc.WithServerKeyManager(serverKeys),
		tlrpc.WithMaxLayer(217),
	)

	gen.RegisterHelpServer(srv, &scenarioHelpService{})

	notifyID := (&gen.HelpGetConfigRequest{}).ConstructorID()
	srv.RegisterMethod(notifyID, func(ctx context.Context, obj tlrpc.TLObject) (interface{}, error) {
		if conn, ok := tlrpc.ConnFromContext(ctx); ok {
			go func() {
				time.Sleep(10 * time.Millisecond)
				_ = conn.Send(&compatPush{Value: 7})
			}()
		}
		return (&scenarioHelpService{}).GetConfig(ctx, obj.(*gen.HelpGetConfigRequest))
	})

	tcpLis, err := (&transport.TCPTransport{AllowObfuscation: true}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.ServeTransport(tcpLis) }()
	t.Cleanup(func() {
		_ = srv.Stop()
		_ = tcpLis.Close()
	})

	constructors := gen.GetStaticConstructors()
	constructors[compatPushID] = func() tlrpc.TLObject { return &compatPush{} }

	cli, err := client.DialTCP(tcpLis.Addr().String(), client.CodecAbridged,
		client.WithServerKey(compatKey),
		client.WithConstructors(constructors),
	)
	if err != nil {
		t.Fatalf("dial client: %v", err)
	}
	defer func() { _ = cli.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Handshake(ctx); err != nil {
		t.Fatalf("handshake: %v", err)
	}

	if _, err := cli.InvokeWrapped(ctx, 217, defaultInitParams(), &gen.HelpGetConfigRequest{}, false); err != nil {
		t.Fatalf("help.getConfig: %v", err)
	}

	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelRead()
	obj, err := cli.ReadOne(readCtx)
	if err != nil {
		t.Fatalf("read push: %v", err)
	}
	push, ok := obj.(*compatPush)
	if !ok {
		t.Fatalf("unexpected push type %T", obj)
	}
	if push.Value != 7 {
		t.Fatalf("unexpected push value %d", push.Value)
	}
}
