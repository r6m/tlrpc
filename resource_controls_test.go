package tlrpc

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/r6m/tlrpc/transport"
)

func TestControlledConnEnforcesCompletePayloadBounds(t *testing.T) {
	base := newResourceTestConn()
	base.reads = [][]byte{{1, 2, 3, 4}, {1, 2, 3, 4, 5}}
	conn := NewServer(WithMaxMessageSize(4)).controlConn(base)

	payload, err := conn.ReadMessage(0)
	if err != nil {
		t.Fatalf("read at limit: %v", err)
	}
	if len(payload) != 4 {
		t.Fatalf("read %d bytes, want 4", len(payload))
	}
	if _, err := conn.ReadMessage(0); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("oversize read error = %v, want ErrMessageTooLarge", err)
	}

	if err := conn.WriteMessage([]byte{1, 2, 3, 4}); err != nil {
		t.Fatalf("write at limit: %v", err)
	}
	if err := conn.WriteMessage([]byte{1, 2, 3, 4, 5}); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("oversize write error = %v, want ErrMessageTooLarge", err)
	}
	base.mu.Lock()
	writes := len(base.writes)
	base.mu.Unlock()
	if writes != 1 {
		t.Fatalf("underlying writes = %d, want only the in-limit payload", writes)
	}
}

func TestControlledConnUsesIndependentOperationDeadlines(t *testing.T) {
	base := newResourceTestConn()
	base.readBlock = make(chan struct{})
	base.readDeadlineSet = make(chan struct{}, 1)
	base.inspectWrite = func() error {
		base.mu.Lock()
		defer base.mu.Unlock()
		if base.readDeadline.IsZero() {
			return errors.New("write cleared the active read deadline")
		}
		if base.writeDeadline.IsZero() {
			return errors.New("write deadline was not set")
		}
		return nil
	}
	conn := NewServer(
		WithReadTimeout(time.Second),
		WithWriteTimeout(time.Second),
	).controlConn(base)

	readDone := make(chan error, 1)
	go func() {
		_, err := conn.ReadMessage(0)
		readDone <- err
	}()
	select {
	case <-base.readDeadlineSet:
	case <-time.After(time.Second):
		t.Fatal("read deadline was not installed")
	}

	if err := conn.WriteMessage([]byte{1, 2, 3, 4}); err != nil {
		t.Fatalf("concurrent write: %v", err)
	}
	base.mu.Lock()
	if base.readDeadline.IsZero() {
		base.mu.Unlock()
		t.Fatal("write operation corrupted read deadline")
	}
	if !base.writeDeadline.IsZero() {
		base.mu.Unlock()
		t.Fatal("write deadline was not cleared after write")
	}
	base.mu.Unlock()

	close(base.readBlock)
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatalf("read: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("read did not finish")
	}
	base.mu.Lock()
	defer base.mu.Unlock()
	if !base.readDeadline.IsZero() {
		t.Fatal("read deadline was not cleared after read")
	}
}

func TestControlledConnRejectsCombinedDeadlineFallback(t *testing.T) {
	base := &combinedDeadlineOnlyConn{base: newResourceTestConn()}
	readConn := NewServer(WithReadTimeout(time.Second)).controlConn(base)
	if _, err := readConn.ReadMessage(0); !errors.Is(err, ErrDirectionalDeadlinesUnsupported) {
		t.Fatalf("read error = %v, want ErrDirectionalDeadlinesUnsupported", err)
	}
	writeConn := NewServer(WithWriteTimeout(time.Second)).controlConn(base)
	if err := writeConn.WriteMessage([]byte{1, 2, 3, 4}); !errors.Is(err, ErrDirectionalDeadlinesUnsupported) {
		t.Fatalf("write error = %v, want ErrDirectionalDeadlinesUnsupported", err)
	}
	if got := base.combinedDeadlineCalls.Load(); got != 0 {
		t.Fatalf("SetDeadline called %d times; directional options must not alter both directions", got)
	}
}

func TestControlledConnSerializesWritesAndBoundsLockWait(t *testing.T) {
	base := newResourceTestConn()
	base.writeStarted = make(chan struct{}, 1)
	base.writeBlock = make(chan struct{})
	conn := NewServer(WithWriteTimeout(100 * time.Millisecond)).controlConn(base)

	firstDone := make(chan error, 1)
	go func() { firstDone <- conn.WriteMessage([]byte{1}) }()
	select {
	case <-base.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("first write did not start")
	}

	started := time.Now()
	err := conn.WriteMessage([]byte{2})
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("queued write error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed < 50*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("queued write waited %v, want bounded wait near configured timeout", elapsed)
	}
	base.mu.Lock()
	if base.maxActiveWrites != 1 {
		base.mu.Unlock()
		t.Fatalf("maximum concurrent underlying writes = %d, want 1", base.maxActiveWrites)
	}
	base.mu.Unlock()

	close(base.writeBlock)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first write: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first write did not finish")
	}
}

func TestMaxConcurrentStreamsBoundsApplicationHandlers(t *testing.T) {
	s := NewServer(WithMaxConcurrentStreams(1))
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	handler := func(ctx context.Context) error {
		if err := s.acquireHandler(ctx); err != nil {
			return err
		}
		defer s.releaseHandler()
		entered <- struct{}{}
		<-release
		return nil
	}

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- handler(context.Background())
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first handler did not enter")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := handler(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second handler error = %v, want context deadline exceeded", err)
	}
	select {
	case <-entered:
		t.Fatal("second handler ran while the server-wide slot was occupied")
	default:
	}

	close(release)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first handler: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first handler did not finish")
	}
}

func TestHandlerAdmissionIsCanceledByShutdown(t *testing.T) {
	s := NewServer(WithMaxConcurrentStreams(1))
	if err := s.acquireHandler(context.Background()); err != nil {
		t.Fatalf("occupy handler slot: %v", err)
	}
	waiter := make(chan error, 1)
	go func() { waiter <- s.acquireHandler(context.Background()) }()

	if err := s.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	select {
	case err := <-waiter:
		if !errors.Is(err, ErrServerStopped) {
			t.Fatalf("waiter error = %v, want ErrServerStopped", err)
		}
	case <-time.After(time.Second):
		t.Fatal("handler waiter remained blocked after shutdown")
	}
	s.releaseHandler()
}

type resourceTestConn struct {
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.Mutex
	reads           [][]byte
	writes          [][]byte
	readBlock       chan struct{}
	writeBlock      chan struct{}
	writeStarted    chan struct{}
	readDeadlineSet chan struct{}
	readDeadline    time.Time
	writeDeadline   time.Time
	activeWrites    int
	maxActiveWrites int
	inspectWrite    func() error
}

func newResourceTestConn() *resourceTestConn {
	ctx, cancel := context.WithCancel(context.Background())
	return &resourceTestConn{ctx: ctx, cancel: cancel}
}

func (c *resourceTestConn) ReadMessage(maxPayloadBytes int) ([]byte, error) {
	if c.readBlock != nil {
		<-c.readBlock
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.reads) == 0 {
		return nil, nil
	}
	payload := append([]byte(nil), c.reads[0]...)
	c.reads = c.reads[1:]
	if maxPayloadBytes > 0 && len(payload) > maxPayloadBytes {
		return nil, transport.ErrPayloadTooLarge
	}
	return payload, nil
}

func (c *resourceTestConn) WriteMessage(payload []byte) error {
	c.mu.Lock()
	c.activeWrites++
	if c.activeWrites > c.maxActiveWrites {
		c.maxActiveWrites = c.activeWrites
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.activeWrites--
		c.mu.Unlock()
	}()
	if c.inspectWrite != nil {
		if err := c.inspectWrite(); err != nil {
			return err
		}
	}
	if c.writeStarted != nil {
		select {
		case c.writeStarted <- struct{}{}:
		default:
		}
	}
	if c.writeBlock != nil {
		<-c.writeBlock
	}
	c.mu.Lock()
	c.writes = append(c.writes, append([]byte(nil), payload...))
	c.mu.Unlock()
	return nil
}

func (c *resourceTestConn) Close() error {
	c.cancel()
	return nil
}
func (c *resourceTestConn) LocalAddr() net.Addr  { return resourceTestAddr("local") }
func (c *resourceTestConn) RemoteAddr() net.Addr { return resourceTestAddr("remote") }
func (c *resourceTestConn) SetDeadline(time.Time) error {
	return errors.New("combined deadline must not be used")
}
func (c *resourceTestConn) SetReadDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.readDeadline = deadline
	c.mu.Unlock()
	if !deadline.IsZero() && c.readDeadlineSet != nil {
		select {
		case c.readDeadlineSet <- struct{}{}:
		default:
		}
	}
	return nil
}
func (c *resourceTestConn) SetWriteDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.writeDeadline = deadline
	c.mu.Unlock()
	return nil
}
func (c *resourceTestConn) Context() context.Context { return c.ctx }

type combinedDeadlineOnlyConn struct {
	base                  *resourceTestConn
	combinedDeadlineCalls atomic.Int32
}

func (c *combinedDeadlineOnlyConn) ReadMessage(maxPayloadBytes int) ([]byte, error) {
	return c.base.ReadMessage(maxPayloadBytes)
}
func (c *combinedDeadlineOnlyConn) WriteMessage(payload []byte) error {
	return c.base.WriteMessage(payload)
}
func (c *combinedDeadlineOnlyConn) Close() error             { return c.base.Close() }
func (c *combinedDeadlineOnlyConn) LocalAddr() net.Addr      { return c.base.LocalAddr() }
func (c *combinedDeadlineOnlyConn) RemoteAddr() net.Addr     { return c.base.RemoteAddr() }
func (c *combinedDeadlineOnlyConn) Context() context.Context { return c.base.Context() }
func (c *combinedDeadlineOnlyConn) SetDeadline(time.Time) error {
	c.combinedDeadlineCalls.Add(1)
	return nil
}

type resourceTestAddr string

func (a resourceTestAddr) Network() string { return "resource-test" }
func (a resourceTestAddr) String() string  { return string(a) }

var _ io.Closer = (*resourceTestConn)(nil)
