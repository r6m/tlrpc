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
	sessions := session.NewMemoryStore()

	srv := tlrpc.NewServer(
		tlrpc.WithAuthKeyManager(authKeys),
		tlrpc.WithSessionStore(sessions),
		tlrpc.WithServerKeyManager(serverKeys),
	)

	srv.RegisterService(tlrpc.ServiceDesc{
		ServiceName: "compat.push.Help",
		SchemaLayer: gen.SchemaLayer,
		HandlerType: (*minimalPushHelpServer)(nil),
		Methods: []tlrpc.MethodDesc{{
			MethodName:    "GetConfig",
			ConstructorID: (&gen.HelpGetConfigRequest{}).ConstructorID(),
			NewRequest:    func() tlrpc.TLObject { return &gen.HelpGetConfigRequest{} },
			Handler:       minimalPushGetConfigHandler,
		}},
	}, minimalPushHelpService{})

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

type minimalPushHelpServer interface {
	GetConfig(context.Context, *gen.HelpGetConfigRequest) (*gen.Config, error)
}

type minimalPushHelpService struct{}

func (minimalPushHelpService) GetConfig(ctx context.Context, request *gen.HelpGetConfigRequest) (*gen.Config, error) {
	sender, ok := tlrpc.SenderFromContext(ctx)
	if !ok {
		return nil, tlrpc.ErrSenderUnavailable
	}
	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = sender.Send(context.Background(), &compatPush{Value: 7})
	}()
	return (&scenarioHelpService{}).GetConfig(ctx, request)
}

func minimalPushGetConfigHandler(service interface{}, ctx context.Context, request *gen.HelpGetConfigRequest) (*gen.Config, error) {
	return service.(minimalPushHelpServer).GetConfig(ctx, request)
}
