package transport

import (
	"bytes"
	"fmt"
	"testing"
)

func TestWebSocketTransport(t *testing.T) {
	transport := &WebSocketTransport{}
	listener, err := transport.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	addr := fmt.Sprintf("ws://%s", listener.Addr().String())
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
			serverErr <- fmt.Errorf("server received unexpected message")
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
