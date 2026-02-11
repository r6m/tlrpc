// Package transport provides TCP transport implementation.
package transport

import (
	"crypto/tls"
	"net"
	"time"
)

// TCPTransport provides TCP transport with optional TLS.
type TCPTransport struct {
	TLSConfig *tls.Config
}

// NewTCPTransport creates a new TCP transport.
func NewTCPTransport() *TCPTransport {
	return &TCPTransport{}
}

// NewTLS creates a new TCP transport with TLS.
func NewTLS(config *tls.Config) *TCPTransport {
	return &TCPTransport{
		TLSConfig: config,
	}
}

// Listen starts listening on the given address.
func (t *TCPTransport) Listen(addr string) (Listener, error) {
	var lis net.Listener
	var err error

	if t.TLSConfig != nil {
		lis, err = tls.Listen("tcp", addr, t.TLSConfig)
	} else {
		lis, err = net.Listen("tcp", addr)
	}

	if err != nil {
		return nil, err
	}

	return &tcpListener{Listener: lis}, nil
}

// Dial connects to the given address.
func (t *TCPTransport) Dial(addr string) (Conn, error) {
	var conn net.Conn
	var err error

	if t.TLSConfig != nil {
		conn, err = tls.Dial("tcp", addr, t.TLSConfig)
	} else {
		conn, err = net.Dial("tcp", addr)
	}

	if err != nil {
		return nil, err
	}

	return &tcpConn{Conn: conn}, nil
}