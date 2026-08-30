package tlrpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/r6m/tlrpc/transport"
)

var (
	// ErrMessageTooLarge reports a complete MTProto payload that exceeds the
	// configured inbound or outbound limit.
	ErrMessageTooLarge = errors.New("tlrpc: MTProto payload exceeds maximum message size")
	// ErrDirectionalDeadlinesUnsupported reports a transport that cannot apply
	// read and write deadlines independently. TLRPC refuses to use SetDeadline
	// as a fallback because doing so can interrupt traffic in the other direction.
	ErrDirectionalDeadlinesUnsupported = errors.New("tlrpc: transport does not support directional deadlines")
	// ErrServerStopped is returned when shutdown cancels a handler waiting for a
	// server-wide application execution slot.
	ErrServerStopped = errors.New("tlrpc: server stopped")
)

type messageSizeError struct {
	direction string
	size      int
	limit     int
}

func (e *messageSizeError) Error() string {
	return fmt.Sprintf("%s: %s payload is %d bytes, limit is %d", ErrMessageTooLarge, e.direction, e.size, e.limit)
}

func (e *messageSizeError) Unwrap() error { return ErrMessageTooLarge }

type readDeadlineSetter interface {
	SetReadDeadline(time.Time) error
}

type writeDeadlineSetter interface {
	SetWriteDeadline(time.Time) error
}

// controlledConn is the single runtime I/O boundary for an accepted
// connection. It enforces complete-payload limits, directional operation
// deadlines, and one synchronous serialized writer. Because writes are not
// queued, callers themselves receive backpressure and there is no hidden
// buffer to overflow.
type controlledConn struct {
	base           transport.Conn
	maxMessageSize int
	readTimeout    time.Duration
	writeTimeout   time.Duration
	writePermit    chan struct{}
}

func (s *Server) controlConn(base transport.Conn) *controlledConn {
	c := &controlledConn{
		base:           base,
		maxMessageSize: s.maxMessageSize,
		readTimeout:    s.readTimeout,
		writeTimeout:   s.writeTimeout,
		writePermit:    make(chan struct{}, 1),
	}
	c.writePermit <- struct{}{}
	return c
}

func (c *controlledConn) ReadMessage(maxPayloadBytes int) ([]byte, error) {
	clear, err := c.beginRead()
	if err != nil {
		return nil, err
	}
	if c.maxMessageSize > 0 && (maxPayloadBytes <= 0 || c.maxMessageSize < maxPayloadBytes) {
		maxPayloadBytes = c.maxMessageSize
	}
	payload, readErr := c.base.ReadMessage(maxPayloadBytes)
	clearErr := clear()
	if readErr != nil {
		if errors.Is(readErr, transport.ErrPayloadTooLarge) {
			return nil, fmt.Errorf("%w: inbound payload exceeds limit %d", ErrMessageTooLarge, maxPayloadBytes)
		}
		return nil, readErr
	}
	if clearErr != nil {
		return nil, clearErr
	}
	return payload, nil
}

func (c *controlledConn) WriteMessage(payload []byte) error {
	if c.maxMessageSize > 0 && len(payload) > c.maxMessageSize {
		return &messageSizeError{direction: "outbound", size: len(payload), limit: c.maxMessageSize}
	}

	deadline, err := c.acquireWriter()
	if err != nil {
		return err
	}
	defer func() { c.writePermit <- struct{}{} }()

	clear, err := c.beginWrite(deadline)
	if err != nil {
		return err
	}
	writeErr := c.base.WriteMessage(payload)
	clearErr := clear()
	if writeErr != nil {
		return writeErr
	}
	return clearErr
}

func (c *controlledConn) acquireWriter() (time.Time, error) {
	ctx := c.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if c.writeTimeout <= 0 {
		select {
		case <-c.writePermit:
			return time.Time{}, nil
		case <-ctx.Done():
			return time.Time{}, ctx.Err()
		}
	}

	deadline := time.Now().Add(c.writeTimeout)
	timer := time.NewTimer(c.writeTimeout)
	defer timer.Stop()
	select {
	case <-c.writePermit:
		return deadline, nil
	case <-timer.C:
		return time.Time{}, os.ErrDeadlineExceeded
	case <-ctx.Done():
		return time.Time{}, ctx.Err()
	}
}

func (c *controlledConn) beginRead() (func() error, error) {
	if c.readTimeout <= 0 {
		return func() error { return nil }, nil
	}
	setter, ok := c.base.(readDeadlineSetter)
	if !ok {
		return nil, ErrDirectionalDeadlinesUnsupported
	}
	if err := setter.SetReadDeadline(time.Now().Add(c.readTimeout)); err != nil {
		return nil, err
	}
	return func() error { return setter.SetReadDeadline(time.Time{}) }, nil
}

func (c *controlledConn) beginWrite(deadline time.Time) (func() error, error) {
	if c.writeTimeout <= 0 {
		return func() error { return nil }, nil
	}
	setter, ok := c.base.(writeDeadlineSetter)
	if !ok {
		return nil, ErrDirectionalDeadlinesUnsupported
	}
	if deadline.IsZero() {
		deadline = time.Now().Add(c.writeTimeout)
	}
	if err := setter.SetWriteDeadline(deadline); err != nil {
		return nil, err
	}
	return func() error { return setter.SetWriteDeadline(time.Time{}) }, nil
}

func (c *controlledConn) Close() error                  { return c.base.Close() }
func (c *controlledConn) LocalAddr() net.Addr           { return c.base.LocalAddr() }
func (c *controlledConn) RemoteAddr() net.Addr          { return c.base.RemoteAddr() }
func (c *controlledConn) Context() context.Context      { return c.base.Context() }
func (c *controlledConn) SetDeadline(t time.Time) error { return c.base.SetDeadline(t) }

func (c *controlledConn) TransportMode() string {
	if provider, ok := c.base.(interface{ TransportMode() string }); ok {
		return provider.TransportMode()
	}
	return ""
}

func (c *controlledConn) SetReadDeadline(t time.Time) error {
	setter, ok := c.base.(readDeadlineSetter)
	if !ok {
		return ErrDirectionalDeadlinesUnsupported
	}
	return setter.SetReadDeadline(t)
}

func (c *controlledConn) SetWriteDeadline(t time.Time) error {
	setter, ok := c.base.(writeDeadlineSetter)
	if !ok {
		return ErrDirectionalDeadlinesUnsupported
	}
	return setter.SetWriteDeadline(t)
}

func (s *Server) acquireHandler(ctx context.Context) error {
	if s.handlerSlots == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-s.shutdownCh:
		return ErrServerStopped
	default:
	}
	select {
	case s.handlerSlots <- struct{}{}:
		select {
		case <-s.shutdownCh:
			<-s.handlerSlots
			return ErrServerStopped
		default:
			return nil
		}
	case <-ctx.Done():
		return ctx.Err()
	case <-s.shutdownCh:
		return ErrServerStopped
	}
}

func (s *Server) releaseHandler() {
	if s.handlerSlots != nil {
		<-s.handlerSlots
	}
}

var _ transport.Conn = (*controlledConn)(nil)
