package runtime

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto/reliability"
	"github.com/r6m/tlrpc/session"
)

func TestConnectionFrameSinkSerializesConcurrentWrites(t *testing.T) {
	connection := &concurrencyCheckingFrameConnection{}
	sink := newConnectionFrameSink(connection)

	const writers = 64
	start := make(chan struct{})
	errs := make(chan error, writers)
	var group sync.WaitGroup
	group.Add(writers)
	for index := 0; index < writers; index++ {
		go func(value byte) {
			defer group.Done()
			<-start
			errs <- sink.WriteFrame(context.Background(), []byte{value})
		}(byte(index))
	}
	close(start)
	group.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("write frame: %v", err)
		}
	}
	if got := connection.maxActive.Load(); got != 1 {
		t.Fatalf("maximum concurrent physical writes = %d, want 1", got)
	}
	if got := connection.writeCount.Load(); got != writers {
		t.Fatalf("physical writes = %d, want %d", got, writers)
	}
}

func TestSessionWriterShutdownDoesNotClosePhysicalConnection(t *testing.T) {
	connection := &concurrencyCheckingFrameConnection{}
	sink := newConnectionFrameSink(connection)
	first := newFrameSinkTestWriter(t, sink, 101)
	second := newFrameSinkTestWriter(t, sink, 202)

	if err := first.writer.Submit(context.Background(), Close{Cause: ErrWriterClosed}); err != nil {
		t.Fatalf("close first writer: %v", err)
	}
	select {
	case <-first.writer.Done():
	case <-time.After(time.Second):
		t.Fatal("first writer did not stop")
	}
	if got := connection.closeCount.Load(); got != 0 {
		t.Fatalf("physical closes after session writer shutdown = %d, want 0", got)
	}

	if err := second.writer.Submit(context.Background(), Push{Body: constructorBody(0x01020304)}); err != nil {
		t.Fatalf("second writer submit after first stopped: %v", err)
	}
	if got := connection.writeCount.Load(); got != 1 {
		t.Fatalf("physical writes after second writer submit = %d, want 1", got)
	}
	if got := connection.closeCount.Load(); got != 0 {
		t.Fatalf("physical closes after second writer submit = %d, want 0", got)
	}
}

func TestConnectionFrameSinkCanceledWriterLeavesBehindStalledWriter(t *testing.T) {
	connection := newStalledFrameConnection()
	sink := newConnectionFrameSink(connection, FrameSinkPolicy{QueueCapacity: 1, WriteTimeout: time.Second})
	firstDone := make(chan error, 1)
	go func() { firstDone <- sink.WriteFrame(context.Background(), []byte{1}) }()
	<-connection.started

	ctx, cancel := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() { secondDone <- sink.WriteFrame(ctx, []byte{2}) }()
	waitForFrameSinkQueue(t, sink, 2)
	cancel()
	select {
	case err := <-secondDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled writer error = %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("canceled writer remained blocked behind physical writer")
	}

	close(connection.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first write: %v", err)
	}
}

func TestConnectionFrameSinkBoundsWaitingWriters(t *testing.T) {
	connection := newStalledFrameConnection()
	sink := newConnectionFrameSink(connection, FrameSinkPolicy{QueueCapacity: 1, WriteTimeout: time.Second})
	firstDone := make(chan error, 1)
	go func() { firstDone <- sink.WriteFrame(context.Background(), []byte{1}) }()
	<-connection.started

	secondCtx, cancelSecond := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() { secondDone <- sink.WriteFrame(secondCtx, []byte{2}) }()
	waitForFrameSinkQueue(t, sink, 2)

	thirdCtx, cancelThird := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelThird()
	started := time.Now()
	err := sink.WriteFrame(thirdCtx, []byte{3})
	if !errors.Is(err, ErrPhysicalWriteQueueFull) || !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("bounded queue error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("bounded queue rejection took %v", elapsed)
	}

	cancelSecond()
	if err := <-secondDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("second writer error = %v", err)
	}
	close(connection.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first write: %v", err)
	}
}

func TestConnectionFrameSinkDeadlineCoversPhysicalWrite(t *testing.T) {
	connection := &deadlineFrameConnection{}
	sink := newConnectionFrameSink(connection, FrameSinkPolicy{QueueCapacity: 1, WriteTimeout: 20 * time.Millisecond})
	started := time.Now()
	err := sink.WriteFrame(context.Background(), []byte{1})
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("physical write error = %v, want deadline", err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("physical deadline took %v", elapsed)
	}
}

func TestConnectionFrameSinkCancellationInterruptsActivePhysicalWrite(t *testing.T) {
	left, right := net.Pipe()
	defer func() { _ = left.Close() }()
	defer func() { _ = right.Close() }()
	connection := &pipeFrameConnection{Conn: left}
	sink := newConnectionFrameSink(connection, FrameSinkPolicy{QueueCapacity: 1, WriteTimeout: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sink.WriteFrame(ctx, make([]byte, 1024)) }()
	time.Sleep(10 * time.Millisecond)
	started := time.Now()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled active write unexpectedly succeeded")
		}
		if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
			t.Fatalf("active cancellation took %v", elapsed)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("active physical write ignored cancellation")
	}
}

func waitForFrameSinkQueue(t *testing.T, sink *connectionFrameSink, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for len(sink.queueSlots) != want {
		if time.Now().After(deadline) {
			t.Fatalf("queue slots = %d, want %d", len(sink.queueSlots), want)
		}
		time.Sleep(time.Millisecond)
	}
}

type frameSinkTestWriter struct {
	writer *Writer
}

func newFrameSinkTestWriter(t *testing.T, sink FrameSink, sessionID int64) frameSinkTestWriter {
	t.Helper()
	var authKey crypto.AuthKey
	for index := range authKey {
		authKey[index] = byte(index + 1)
	}
	key := session.SessionKey{AuthKeyID: authKey.ID(), SessionID: sessionID}
	registry := NewSessionLeaseRegistry(session.NewMemoryStore())
	lease, err := registry.Acquire(context.Background(), key, session.Snapshot{
		AuthKeyID:  key.AuthKeyID,
		SessionID:  key.SessionID,
		ServerSalt: 303,
	})
	if err != nil {
		t.Fatalf("acquire session lease: %v", err)
	}
	retained, err := reliability.New(reliability.Config{Capacity: 8, TTL: time.Minute})
	if err != nil {
		lease.Release()
		t.Fatalf("new reliability store: %v", err)
	}
	writer, err := NewWriter(context.Background(), WriterConfig{
		Lease:       lease,
		AuthKey:     authKey,
		Sink:        sink,
		MessageIDs:  &fixedMessageIDs{next: sessionID * 4},
		Reliability: retained,
	})
	if err != nil {
		lease.Release()
		t.Fatalf("new writer: %v", err)
	}
	t.Cleanup(func() {
		_ = writer.Submit(context.Background(), Close{Cause: ErrWriterClosed})
		select {
		case <-writer.Done():
		case <-time.After(time.Second):
			t.Error("writer did not stop")
		}
		lease.Release()
	})
	return frameSinkTestWriter{writer: writer}
}

type concurrencyCheckingFrameConnection struct {
	active     atomic.Int32
	maxActive  atomic.Int32
	writeCount atomic.Int32
	closeCount atomic.Int32
}

func (*concurrencyCheckingFrameConnection) ReadMessage(int) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func (c *concurrencyCheckingFrameConnection) WriteMessage([]byte) error {
	active := c.active.Add(1)
	defer c.active.Add(-1)
	for {
		maximum := c.maxActive.Load()
		if active <= maximum || c.maxActive.CompareAndSwap(maximum, active) {
			break
		}
	}
	time.Sleep(time.Millisecond)
	c.writeCount.Add(1)
	return nil
}

func (c *concurrencyCheckingFrameConnection) Close() error {
	c.closeCount.Add(1)
	return nil
}

func (*concurrencyCheckingFrameConnection) SetWriteDeadline(time.Time) error { return nil }

func (*concurrencyCheckingFrameConnection) Context() context.Context { return context.Background() }
func (*concurrencyCheckingFrameConnection) LocalAddr() net.Addr      { return nil }
func (*concurrencyCheckingFrameConnection) RemoteAddr() net.Addr     { return nil }

var _ FrameConnection = (*concurrencyCheckingFrameConnection)(nil)

type stalledFrameConnection struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newStalledFrameConnection() *stalledFrameConnection {
	return &stalledFrameConnection{started: make(chan struct{}), release: make(chan struct{})}
}

func (*stalledFrameConnection) ReadMessage(int) ([]byte, error) {
	return nil, errors.New("not implemented")
}
func (c *stalledFrameConnection) WriteMessage([]byte) error {
	c.once.Do(func() { close(c.started) })
	<-c.release
	return nil
}
func (*stalledFrameConnection) Close() error                     { return nil }
func (*stalledFrameConnection) SetWriteDeadline(time.Time) error { return nil }
func (*stalledFrameConnection) Context() context.Context         { return context.Background() }
func (*stalledFrameConnection) LocalAddr() net.Addr              { return nil }
func (*stalledFrameConnection) RemoteAddr() net.Addr             { return nil }

type deadlineFrameConnection struct {
	mu       sync.Mutex
	deadline time.Time
}

func (*deadlineFrameConnection) ReadMessage(int) ([]byte, error) {
	return nil, errors.New("not implemented")
}
func (c *deadlineFrameConnection) WriteMessage([]byte) error {
	c.mu.Lock()
	deadline := c.deadline
	c.mu.Unlock()
	if deadline.IsZero() {
		return errors.New("write deadline was not installed")
	}
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	<-timer.C
	return os.ErrDeadlineExceeded
}
func (*deadlineFrameConnection) Close() error             { return nil }
func (*deadlineFrameConnection) Context() context.Context { return context.Background() }
func (*deadlineFrameConnection) LocalAddr() net.Addr      { return nil }
func (*deadlineFrameConnection) RemoteAddr() net.Addr     { return nil }
func (c *deadlineFrameConnection) SetWriteDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.deadline = deadline
	c.mu.Unlock()
	return nil
}

type pipeFrameConnection struct{ net.Conn }

func (*pipeFrameConnection) ReadMessage(int) ([]byte, error) {
	return nil, errors.New("not implemented")
}
func (c *pipeFrameConnection) WriteMessage(frame []byte) error {
	_, err := c.Write(frame)
	return err
}
func (c *pipeFrameConnection) Context() context.Context { return context.Background() }
