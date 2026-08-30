package tlrpc

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/r6m/tlrpc/transport"
)

func TestServerStopUnblocksServeAndClosesAcceptedConnections(t *testing.T) {
	lis := newLifecycleNetListener()
	s := NewServer()
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- s.Serve(lis)
	}()

	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	lis.accepts <- server

	waitForOwnedConnections(t, s, 1)
	if err := s.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("Serve returned an error during Stop: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve remained blocked in Accept after Stop")
	}

	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("accepted connection remained open after Stop")
	}
	assertLifecycleDrained(t, s)
}

func TestServerStopOwnsTransportLifecycleAndIsConcurrentSafe(t *testing.T) {
	lis := newLifecycleListener()
	conn := newLifecycleConn()
	s := NewServer()
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- s.ServeTransport(lis)
	}()

	lis.accepts <- conn
	select {
	case <-conn.readStarted:
	case <-time.After(time.Second):
		t.Fatal("accepted connection handler did not start")
	}

	const callers = 16
	var wg sync.WaitGroup
	wg.Add(callers)
	errs := make(chan error, callers)
	for range callers {
		go func() {
			defer wg.Done()
			errs <- s.Stop()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Stop returned an error: %v", err)
		}
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("ServeTransport returned an error during Stop: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ServeTransport remained blocked in Accept after Stop")
	}
	select {
	case <-conn.closed:
	case <-time.After(time.Second):
		t.Fatal("Stop did not close the active transport connection")
	}

	if got := lis.closeCalls.Load(); got != 1 {
		t.Fatalf("listener closed %d times, want 1", got)
	}
	if got := conn.closeCalls.Load(); got != 1 {
		t.Fatalf("connection closed %d times, want 1", got)
	}
	if err := lis.Close(); err != nil {
		t.Fatalf("caller Close after Stop: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("caller connection Close after Stop: %v", err)
	}
	assertLifecycleDrained(t, s)
}

func waitForOwnedConnections(t *testing.T, s *Server, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.lifecycleMu.Lock()
		got := len(s.connections)
		s.lifecycleMu.Unlock()
		if got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("server did not own %d connection(s) before deadline", want)
}

func assertLifecycleDrained(t *testing.T, s *Server) {
	t.Helper()
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if got := len(s.listeners); got != 0 {
		t.Errorf("server retains %d listener(s) after shutdown", got)
	}
	if got := len(s.connections); got != 0 {
		t.Errorf("server retains %d connection(s) after shutdown", got)
	}
}

type lifecycleListener struct {
	accepts    chan transport.Conn
	closed     chan struct{}
	closeOnce  sync.Once
	closeCalls atomic.Int32
}

type lifecycleNetListener struct {
	accepts   chan net.Conn
	closed    chan struct{}
	closeOnce sync.Once
}

func newLifecycleNetListener() *lifecycleNetListener {
	return &lifecycleNetListener{
		accepts: make(chan net.Conn),
		closed:  make(chan struct{}),
	}
}

func (l *lifecycleNetListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.accepts:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *lifecycleNetListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (l *lifecycleNetListener) Addr() net.Addr { return lifecycleAddr("net-listener") }

func newLifecycleListener() *lifecycleListener {
	return &lifecycleListener{
		accepts: make(chan transport.Conn),
		closed:  make(chan struct{}),
	}
}

func (l *lifecycleListener) Accept() (transport.Conn, error) {
	select {
	case conn := <-l.accepts:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *lifecycleListener) Close() error {
	l.closeOnce.Do(func() {
		l.closeCalls.Add(1)
		close(l.closed)
	})
	return nil
}

func (l *lifecycleListener) Addr() net.Addr { return lifecycleAddr("listener") }

type lifecycleConn struct {
	ctx         context.Context
	cancel      context.CancelFunc
	closed      chan struct{}
	readStarted chan struct{}
	readOnce    sync.Once
	closeOnce   sync.Once
	closeCalls  atomic.Int32
}

func newLifecycleConn() *lifecycleConn {
	ctx, cancel := context.WithCancel(context.Background())
	return &lifecycleConn{
		ctx:         ctx,
		cancel:      cancel,
		closed:      make(chan struct{}),
		readStarted: make(chan struct{}),
	}
}

func (c *lifecycleConn) ReadMessage(_ int) ([]byte, error) {
	c.readOnce.Do(func() { close(c.readStarted) })
	<-c.closed
	return nil, net.ErrClosed
}

func (c *lifecycleConn) WriteMessage([]byte) error {
	select {
	case <-c.closed:
		return net.ErrClosed
	default:
		return nil
	}
}

func (c *lifecycleConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeCalls.Add(1)
		c.cancel()
		close(c.closed)
	})
	return nil
}

func (c *lifecycleConn) LocalAddr() net.Addr              { return lifecycleAddr("local") }
func (c *lifecycleConn) RemoteAddr() net.Addr             { return lifecycleAddr("remote") }
func (c *lifecycleConn) SetReadDeadline(time.Time) error  { return nil }
func (c *lifecycleConn) SetWriteDeadline(time.Time) error { return nil }
func (c *lifecycleConn) Context() context.Context         { return c.ctx }

type lifecycleAddr string

func (a lifecycleAddr) Network() string { return "lifecycle" }
func (a lifecycleAddr) String() string  { return string(a) }

var _ transport.Listener = (*lifecycleListener)(nil)
var _ transport.Conn = (*lifecycleConn)(nil)
