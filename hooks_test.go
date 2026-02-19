package tlrpc

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/session"
)

type testConn struct {
	ctx context.Context
}

func (t *testConn) ReadMessage() ([]byte, error) { return nil, nil }
func (t *testConn) WriteMessage([]byte) error    { return nil }
func (t *testConn) Close() error                 { return nil }
func (t *testConn) LocalAddr() net.Addr          { return nil }
func (t *testConn) RemoteAddr() net.Addr         { return nil }
func (t *testConn) SetDeadline(time.Time) error  { return nil }
func (t *testConn) Context() context.Context     { return t.ctx }
func (t *testConn) Send(TLObject) error          { return nil }

func TestSessionHooks(t *testing.T) {
	var bound []Binding
	var unbound []Binding
	var mu sync.Mutex

	srv := NewServer(
		WithOnSessionBound(func(b Binding, _ Conn) {
			mu.Lock()
			bound = append(bound, b)
			mu.Unlock()
		}),
		WithOnSessionUnbound(func(b Binding) {
			mu.Lock()
			unbound = append(unbound, b)
			mu.Unlock()
		}),
	)

	sess := &Session{SessionID: 10, ServerSalt: 99, UserID: 123, Layer: 7}
	h := &connHandler{
		server: srv,
		conn:   &testConn{ctx: context.Background()},
	}
	h.state.authKeyID = crypto.KeyID(42)

	binding := Binding{
		AuthKeyID:  int64(h.state.authKeyID),
		SessionID:  sess.SessionID,
		ServerSalt: sess.ServerSalt,
		UserID:     sess.UserID,
		Layer:      sess.Layer,
	}
	h.state.binding = binding
	h.state.onceBound.Do(func() {
		if h.server.onSessionBound != nil {
			h.server.onSessionBound(binding, newServerConn(h.server, h.conn.(TransportConn), &h.state))
		}
	})

	h.onUnbound()

	mu.Lock()
	defer mu.Unlock()
	if len(bound) != 1 {
		t.Fatalf("expected 1 bound hook, got %d", len(bound))
	}
	if len(unbound) != 1 {
		t.Fatalf("expected 1 unbound hook, got %d", len(unbound))
	}
	if bound[0].UserID != sess.UserID || unbound[0].UserID != sess.UserID {
		t.Fatalf("unexpected binding values")
	}
}

func TestBindingFromContext(t *testing.T) {
	sess := &session.Session{SessionID: 55, ServerSalt: 77, UserID: 9, Layer: 2}
	ctx := withSession(context.Background(), sess)
	ctx = withAuthKeyID(ctx, 100)
	ctx = withUserID(ctx, sess.UserID)
	ctx = withLayer(ctx, sess.Layer)

	binding, ok := BindingFromContext(ctx)
	if !ok {
		t.Fatalf("expected binding")
	}
	if binding.AuthKeyID != 100 || binding.SessionID != sess.SessionID || binding.UserID != sess.UserID {
		t.Fatalf("unexpected binding")
	}
}

func TestSessionUnboundWithoutBind(t *testing.T) {
	var called []Binding
	var mu sync.Mutex

	srv := NewServer(
		WithOnSessionUnbound(func(b Binding) {
			mu.Lock()
			called = append(called, b)
			mu.Unlock()
		}),
	)

	h := &connHandler{
		server: srv,
		conn:   &testConn{ctx: context.Background()},
	}

	h.onUnbound()

	mu.Lock()
	defer mu.Unlock()
	if len(called) != 1 {
		t.Fatalf("expected unbound hook to fire once, got %d", len(called))
	}
	if called[0].AuthKeyID != 0 || called[0].SessionID != 0 || called[0].UserID != 0 {
		t.Fatalf("expected empty binding, got %+v", called[0])
	}
}
