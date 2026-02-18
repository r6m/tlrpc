package transport

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketTransport implements MTProto messages over WebSocket.
type WebSocketTransport struct {
	Upgrader websocket.Upgrader
	Protocol Protocol
	Secret   []byte
}

// Listen starts a WebSocket listener.
func (t *WebSocketTransport) Listen(addr string) (Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	upgrader := t.Upgrader
	if upgrader.CheckOrigin == nil {
		upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	}
	upgrader.Subprotocols = ensureSubprotocol(upgrader.Subprotocols, "binary")

	wsListener := &wsListener{
		listener: ln,
		upgrader: upgrader,
		conns:    make(chan Conn, 32),
		errors:   make(chan error, 1),
	}

	wsListener.server = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hasSubprotocol(r.Header.Get("Sec-WebSocket-Protocol"), "binary") {
			http.Error(w, "missing Sec-WebSocket-Protocol: binary", http.StatusBadRequest)
			return
		}
		conn, err := wsListener.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		ws := newWSConn(conn)
		mt := newWSMTProtoConn(ws, NegotiatorConfig{
			AllowObfuscation:   true,
			RequireObfuscation: true,
			Secret:             t.Secret,
		}, false)
		select {
		case wsListener.conns <- mt:
		case <-wsListener.ctx.Done():
			_ = mt.Close()
		}
	})}

	wsListener.ctx, wsListener.cancel = context.WithCancel(context.Background())
	go func() {
		if err := wsListener.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			wsListener.errors <- err
		}
	}()

	return wsListener, nil
}

// Dial connects to a WebSocket server.
func (t *WebSocketTransport) Dial(addr string) (Conn, error) {
	dialer := websocket.Dialer{Subprotocols: []string{"binary"}}
	if len(t.Upgrader.Subprotocols) > 0 {
		dialer.Subprotocols = ensureSubprotocol(t.Upgrader.Subprotocols, "binary")
	}
	conn, _, err := dialer.Dial(addr, nil)
	if err != nil {
		return nil, err
	}
	ws := newWSConn(conn)
	protocol := t.Protocol
	if protocol == ProtocolUnknown {
		protocol = ProtocolPaddedIntermediate
	}
	return newWSMTProtoConn(ws, NegotiatorConfig{
		AllowObfuscation:   true,
		RequireObfuscation: true,
		Secret:             t.Secret,
		Protocol:           protocol,
	}, true), nil
}

type wsListener struct {
	listener net.Listener
	server   *http.Server
	upgrader websocket.Upgrader
	conns    chan Conn
	errors   chan error
	ctx      context.Context
	cancel   context.CancelFunc
}

func (l *wsListener) Accept() (Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case err := <-l.errors:
		return nil, err
	case <-l.ctx.Done():
		return nil, net.ErrClosed
	}
}

func (l *wsListener) Close() error {
	l.cancel()
	if err := l.server.Close(); err != nil {
		return err
	}
	return l.listener.Close()
}

func (l *wsListener) Addr() net.Addr {
	return l.listener.Addr()
}

type wsConn struct {
	conn   *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
}

func newWSConn(conn *websocket.Conn) *wsConn {
	ctx, cancel := context.WithCancel(context.Background())
	return &wsConn{conn: conn, ctx: ctx, cancel: cancel}
}

// Close closes the connection.
func (c *wsConn) Close() error {
	c.cancel()
	return c.conn.Close()
}

// LocalAddr returns local address.
func (c *wsConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

// RemoteAddr returns remote address.
func (c *wsConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// SetDeadline sets read/write deadlines.
func (c *wsConn) SetDeadline(t time.Time) error {
	if err := c.conn.SetReadDeadline(t); err != nil {
		return err
	}
	return c.conn.SetWriteDeadline(t)
}

// Context returns a context canceled on close.
func (c *wsConn) Context() context.Context {
	return c.ctx
}

func ensureSubprotocol(list []string, value string) []string {
	for _, item := range list {
		if item == value {
			return list
		}
	}
	return append(list, value)
}

func hasSubprotocol(headerValue, subprotocol string) bool {
	for _, item := range strings.Split(headerValue, ",") {
		if strings.TrimSpace(item) == subprotocol {
			return true
		}
	}
	return false
}
