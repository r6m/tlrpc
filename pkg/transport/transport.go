// Package transport provides transport layer abstractions.
package transport

import (
	"net"
	"time"
)

// Transport provides network transport abstraction.
type Transport interface {
	Listen(addr string) (Listener, error)
	Dial(addr string) (Conn, error)
}

// Listener listens for incoming connections.
type Listener interface {
	Accept() (Conn, error)
	Close() error
	Addr() net.Addr
}

// Conn represents a network connection.
type Conn interface {
	ReadMessage() ([]byte, error)
	WriteMessage([]byte) error
	Close() error
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	SetDeadline(t time.Time) error
}

// tcpTransport implements Transport for TCP.
type tcpTransport struct{}

// NewTCP creates a new TCP transport.
func NewTCP() Transport {
	return &tcpTransport{}
}

// Listen starts listening on the given address.
func (t *tcpTransport) Listen(addr string) (Listener, error) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &tcpListener{Listener: lis}, nil
}

// Dial connects to the given address.
func (t *tcpTransport) Dial(addr string) (Conn, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &tcpConn{Conn: conn}, nil
}

// tcpListener wraps net.Listener.
type tcpListener struct {
	net.Listener
}

func (l *tcpListener) Accept() (Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &tcpConn{Conn: conn}, nil
}

// tcpConn wraps net.Conn.
type tcpConn struct {
	net.Conn
}

func (c *tcpConn) ReadMessage() ([]byte, error) {
	// TODO: Implement proper message framing
	// For now, read until EOF
	buf := make([]byte, 4096)
	n, err := c.Conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func (c *tcpConn) WriteMessage(data []byte) error {
	// TODO: Implement proper message framing
	_, err := c.Conn.Write(data)
	return err
}