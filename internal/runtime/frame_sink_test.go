package runtime

import (
	"context"
	"errors"
	"net"
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

func (*concurrencyCheckingFrameConnection) Context() context.Context { return context.Background() }
func (*concurrencyCheckingFrameConnection) LocalAddr() net.Addr      { return nil }
func (*concurrencyCheckingFrameConnection) RemoteAddr() net.Addr     { return nil }

var _ FrameConnection = (*concurrencyCheckingFrameConnection)(nil)
