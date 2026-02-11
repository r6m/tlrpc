// Package transport provides WebSocket transport implementation.
package transport

import (
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketTransport provides WebSocket transport.
type WebSocketTransport struct {
	upgrader websocket.Upgrader
}

// NewWebSocket creates a new WebSocket transport.
func NewWebSocket() *WebSocketTransport {
	return &WebSocketTransport{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// TODO: Implement proper origin checking
				return true
			},
		},
	}
}

// Listen starts listening on the given address.
func (t *WebSocketTransport) Listen(addr string) (Listener, error) {
	// WebSocket is typically used over HTTP, so we need an HTTP server
	// This is a simplified implementation
	return nil, ErrNotImplemented
}

// Dial connects to the given address.
func (t *WebSocketTransport) Dial(addr string) (Conn, error) {
	// For client connections
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(addr, nil)
	if err != nil {
		return nil, err
	}

	return &wsConn{conn: conn}, nil
}

// wsConn wraps websocket.Conn.
type wsConn struct {
	conn *websocket.Conn
}

func (c *wsConn) ReadMessage() ([]byte, error) {
	_, data, err := c.conn.ReadMessage()
	return data, err
}

func (c *wsConn) WriteMessage(data []byte) error {
	return c.conn.WriteMessage(websocket.BinaryMessage, data)
}

func (c *wsConn) Close() error {
	return c.conn.Close()
}

func (c *wsConn) LocalAddr() net.Addr {
	// WebSocket doesn't provide local addr
	return nil
}

func (c *wsConn) RemoteAddr() net.Addr {
	// WebSocket doesn't provide remote addr
	return nil
}

func (c *wsConn) SetDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

// ErrNotImplemented indicates the feature is not implemented.
var ErrNotImplemented = websocket.ErrBadHandshake