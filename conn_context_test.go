package tlrpc

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/r6m/tlrpc/transport"
)

type fakeConn struct {
	ctx context.Context
}

func (f *fakeConn) ReadMessage() ([]byte, error) { return nil, nil }
func (f *fakeConn) WriteMessage([]byte) error    { return nil }
func (f *fakeConn) Close() error                 { return nil }
func (f *fakeConn) LocalAddr() net.Addr          { return nil }
func (f *fakeConn) RemoteAddr() net.Addr         { return nil }
func (f *fakeConn) SetDeadline(time.Time) error  { return nil }
func (f *fakeConn) Context() context.Context     { return f.ctx }
func (f *fakeConn) Send(TLObject) error          { return nil }

var _ transport.Conn = (*fakeConn)(nil)

func TestConnFromContext(t *testing.T) {
	ctx := context.Background()
	if _, ok := ConnFromContext(ctx); ok {
		t.Fatalf("expected no conn")
	}

	conn := &fakeConn{ctx: ctx}
	ctx = withConn(ctx, conn)
	got, ok := ConnFromContext(ctx)
	if !ok {
		t.Fatalf("expected conn")
	}
	if got != conn {
		t.Fatalf("unexpected conn")
	}
}
