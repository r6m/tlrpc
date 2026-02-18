package transport

import (
	"net"
	"time"
)

// TCPTransport implements MTProto framing over TCP.
type TCPTransport struct {
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	Protocol           Protocol
	AllowObfuscation   bool
	RequireObfuscation bool
	Secret             []byte
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
	return newClientTCPConn(conn, t), nil
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

func newTCPConn(conn net.Conn, transport *TCPTransport) Conn {
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(30 * time.Second)
	}
	config := NegotiatorConfig{
		AllowObfuscation:   transportAllowObfuscation(transport),
		RequireObfuscation: transportRequireObfuscation(transport),
		Secret:             transport.Secret,
	}
	return NewMTProtoConn(conn, config)
}

func newClientTCPConn(conn net.Conn, transport *TCPTransport) Conn {
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(30 * time.Second)
	}
	config := NegotiatorConfig{
		AllowObfuscation:   transportAllowObfuscation(transport),
		RequireObfuscation: transportRequireObfuscation(transport),
		Secret:             transport.Secret,
		Protocol:           transport.Protocol,
	}
	return NewClientMTProtoConn(conn, config)
}

func transportAllowObfuscation(t *TCPTransport) bool {
	if t.RequireObfuscation {
		return true
	}
	if t.AllowObfuscation {
		return true
	}
	return true
}

func transportRequireObfuscation(t *TCPTransport) bool {
	return t.RequireObfuscation
}
