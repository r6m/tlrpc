package transport

import (
	"bytes"
	"errors"
	"net"
	"testing"
)

func TestMTProtoConnAbridged(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = client.Close() }()

	srv := NewMTProtoConn(server, NegotiatorConfig{})
	cli := NewClientMTProtoConn(client, NegotiatorConfig{Protocol: ProtocolAbridged})

	done := make(chan error, 1)
	go func() {
		msg, err := srv.ReadMessage()
		if err != nil {
			done <- err
			return
		}
		if !bytes.Equal(msg, []byte("helloooo")) {
			done <- errors.New("unexpected payload")
			return
		}
		done <- srv.WriteMessage([]byte("world!!!"))
	}()

	if err := cli.WriteMessage([]byte("helloooo")); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp, err := cli.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(resp, []byte("world!!!")) {
		t.Fatalf("response mismatch")
	}
	if err := <-done; err != nil {
		t.Fatalf("server error: %v", err)
	}
}
