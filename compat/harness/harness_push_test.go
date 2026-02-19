package harness

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/examples/gen"
	"github.com/r6m/tlrpc/mtproto"
)

const pushPingID uint32 = 0x7f00a001

type pushPing struct {
	Value int32
}

func (p *pushPing) ConstructorID() uint32 { return pushPingID }
func (p *pushPing) Method() string        { return "" }
func (p *pushPing) TLName() string        { return "test.pushPing" }

func (p *pushPing) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, p.ConstructorID()); err != nil {
		return err
	}
	return mtproto.WriteInt32(w, p.Value)
}

func (p *pushPing) DeserializeTL(r io.Reader) error {
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

func TestHarnessLocalPushDelivery(t *testing.T) {
	srv, err := Start()
	if err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	cli, err := DialTCP(srv.TCPAddr, map[uint32]func() tlrpc.TLObject{
		pushPingID: func() tlrpc.TLObject { return &pushPing{} },
	})
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

	info := cli.Session()
	sess, err := srv.Sessions.Get(info.AuthKeyID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	sess.UserID = 1
	if err := srv.Sessions.Save(sess); err != nil {
		t.Fatalf("save session: %v", err)
	}

	if _, err := cli.InvokeWrapped(ctx, 217, defaultInitParams(), &gen.HelpGetConfigRequest{}, false); err != nil {
		t.Fatalf("help.getConfig: %v", err)
	}

	if err := srv.Server.Publish(1, &pushPing{Value: 42}); err != nil {
		t.Fatalf("publish push: %v", err)
	}

	readCtx, cancelRead := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelRead()
	obj, err := cli.ReadOne(readCtx)
	if err != nil {
		t.Fatalf("read push: %v", err)
	}
	push, ok := obj.(*pushPing)
	if !ok {
		t.Fatalf("unexpected push type %T", obj)
	}
	if push.Value != 42 {
		t.Fatalf("unexpected push value %d", push.Value)
	}
}
