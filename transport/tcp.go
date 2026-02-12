package transport

import (
	"bufio"
	"context"
	"net"
	"time"
)

// TCPTransport implements MTProto framing over TCP.
type TCPTransport struct {
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// Listen starts a TCP listener.
func (t *TCPTransport) Listen(addr string) (Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &tcpListener{Listener: ln, transport: t}, nil
}

// Dial connects to a TCP address.
func (t *TCPTransport) Dial(addr string) (Conn, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return newTCPConn(conn, t), nil
}

type tcpListener struct {
	net.Listener
	transport *TCPTransport
}

func (l *tcpListener) Accept() (Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return newTCPConn(conn, l.transport), nil
}

type tcpConn struct {
	net.Conn
	r         *bufio.Reader
	w         *bufio.Writer
	ctx       context.Context
	cancel    context.CancelFunc
	transport *TCPTransport
}

func newTCPConn(conn net.Conn, transport *TCPTransport) *tcpConn {
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(30 * time.Second)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &tcpConn{
		Conn:      conn,
		r:         bufio.NewReader(conn),
		w:         bufio.NewWriter(conn),
		ctx:       ctx,
		cancel:    cancel,
		transport: transport,
	}
}

// ReadMessage reads a framed MTProto message.
func (c *tcpConn) ReadMessage() ([]byte, error) {
	if c.transport != nil && c.transport.ReadTimeout > 0 {
		_ = c.Conn.SetReadDeadline(time.Now().Add(c.transport.ReadTimeout))
	}
	return readFrame(c.r)
}

// WriteMessage writes a framed MTProto message.
func (c *tcpConn) WriteMessage(payload []byte) error {
	if c.transport != nil && c.transport.WriteTimeout > 0 {
		_ = c.Conn.SetWriteDeadline(time.Now().Add(c.transport.WriteTimeout))
	}
	if err := writeFrame(c.w, payload); err != nil {
		return err
	}
	return c.w.Flush()
}

// Close closes the connection.
func (c *tcpConn) Close() error {
	c.cancel()
	return c.Conn.Close()
}

// Context returns a context canceled on close.
func (c *tcpConn) Context() context.Context {
	return c.ctx
}
