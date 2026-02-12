package transport

import (
	"context"
	"net"
	"time"
)

// Transport creates listeners and connections.
type Transport interface {
	Listen(addr string) (Listener, error)
	Dial(addr string) (Conn, error)
}

// Listener accepts connections.
type Listener interface {
	Accept() (Conn, error)
	Close() error
	Addr() net.Addr
}

// Conn is a transport connection.
type Conn interface {
	// ReadMessage reads a complete message (MTProto frame).
	ReadMessage() ([]byte, error)

	// WriteMessage writes a complete message.
	WriteMessage([]byte) error

	// Close closes the connection.
	Close() error

	// LocalAddr returns local address.
	LocalAddr() net.Addr

	// RemoteAddr returns remote address.
	RemoteAddr() net.Addr

	// SetDeadline sets read/write deadlines.
	SetDeadline(t time.Time) error

	// Context returns connection context (cancelled on close).
	Context() context.Context
}
