package tlrpc

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/r6m/tlrpc/transport"
)

func TestConnectionCloseErrors(t *testing.T) {
	t.Run("normal probe close is quiet", func(t *testing.T) {
		logger := &mockLogger{}
		server := NewServer(WithLogger(logger))
		conn := &connectionCloseTestConn{readErr: fmt.Errorf("probe disconnected: %w", io.EOF)}
		if !server.serveConn(conn) {
			t.Fatal("serveConn rejected connection")
		}
		waitForConnectionClose(t, server)
		if len(logger.errorMsgs) != 0 {
			t.Fatalf("error logs = %v, want none for normal close", logger.errorMsgs)
		}
		if got := classifyConnectionClose(conn.readErr, false); got != "closed" {
			t.Fatalf("close reason = %q, want closed", got)
		}
		if err := server.Stop(); err != nil {
			t.Fatalf("stop: %v", err)
		}
	})

	t.Run("unexpected EOF remains diagnosable", func(t *testing.T) {
		logger := &mockLogger{}
		server := NewServer(WithLogger(logger))
		conn := &connectionCloseTestConn{readErr: io.ErrUnexpectedEOF}
		if !server.serveConn(conn) {
			t.Fatal("serveConn rejected connection")
		}
		waitForConnectionClose(t, server)
		if len(logger.errorMsgs) == 0 {
			t.Fatal("connection error was not logged")
		}
		if got := classifyConnectionClose(conn.readErr, false); got != "failed" {
			t.Fatalf("close reason = %q, want failed", got)
		}
		if err := server.Stop(); err != nil {
			t.Fatalf("stop: %v", err)
		}
	})
}

func waitForConnectionClose(t *testing.T, server *Server) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		server.lifecycleMu.Lock()
		closed := len(server.connectionStates) == 0
		server.lifecycleMu.Unlock()
		if closed {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("connection did not close")
}

type connectionCloseTestConn struct {
	readErr error
}

func (c *connectionCloseTestConn) ReadMessage(int) ([]byte, error) { return nil, c.readErr }
func (*connectionCloseTestConn) WriteMessage([]byte) error         { return nil }
func (*connectionCloseTestConn) Close() error                      { return nil }
func (*connectionCloseTestConn) LocalAddr() net.Addr               { return connectionCloseTestAddr("local") }
func (*connectionCloseTestConn) RemoteAddr() net.Addr              { return connectionCloseTestAddr("remote") }
func (*connectionCloseTestConn) SetReadDeadline(time.Time) error   { return nil }
func (*connectionCloseTestConn) SetWriteDeadline(time.Time) error  { return nil }
func (*connectionCloseTestConn) Context() context.Context          { return context.Background() }

type connectionCloseTestAddr string

func (connectionCloseTestAddr) Network() string  { return "connection-close-test" }
func (a connectionCloseTestAddr) String() string { return string(a) }

var _ transport.Conn = (*connectionCloseTestConn)(nil)
