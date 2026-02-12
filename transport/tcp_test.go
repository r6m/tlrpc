package transport

import (
	"bytes"
	"errors"
	"net"
	"testing"
)

func TestTCPTransport(t *testing.T) {
	transport := &TCPTransport{}
	listener, err := transport.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()
	serverErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		msg, err := conn.ReadMessage()
		if err != nil {
			serverErr <- err
			return
		}
		if !bytes.Equal(msg, []byte("hello")) {
			serverErr <- errors.New("server received unexpected message")
			return
		}
		serverErr <- conn.WriteMessage([]byte("world"))
	}()

	clientConn, err := transport.Dial(addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer clientConn.Close()
	if err := clientConn.WriteMessage([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(resp, []byte("world")) {
		t.Fatalf("response mismatch")
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

func TestTCPConnContextCancel(t *testing.T) {
	transport := &TCPTransport{}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	serverConn, err := listener.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}

	client := newTCPConn(conn, transport)
	client.Close()
	select {
	case <-client.Context().Done():
	default:
		t.Fatalf("context not cancelled")
	}
	_ = serverConn.Close()
}
