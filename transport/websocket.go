package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	defaultWebSocketAcceptQueue   = 64
	defaultWebSocketReadTimeout   = 5 * time.Second
	defaultWebSocketIdleTimeout   = 30 * time.Second
	defaultWebSocketMaxHeaderSize = 8 << 10
)

// WebSocketTransport implements MTProto messages over WebSocket.
type WebSocketTransport struct {
	Upgrader            websocket.Upgrader
	Protocol            Protocol
	Secret              []byte
	OriginPolicy        WebSocketOriginPolicy
	AcceptQueueCapacity int
	ReadTimeout         time.Duration
	IdleTimeout         time.Duration
	MaxHeaderBytes      int
}

type WebSocketOriginPolicy struct {
	AllowAny       bool
	AllowMissing   bool
	AllowedOrigins []string
}

// Listen starts a WebSocket listener.
func (t *WebSocketTransport) Listen(addr string) (Listener, error) {
	if err := t.validateOriginPolicy(); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	upgrader := t.Upgrader
	upgrader.CheckOrigin = t.upgradeOriginChecker()
	upgrader.Subprotocols = ensureSubprotocol(upgrader.Subprotocols, "binary")
	queueCapacity := t.AcceptQueueCapacity
	if queueCapacity <= 0 {
		queueCapacity = defaultWebSocketAcceptQueue
	}

	wsListener := &wsListener{
		listener: ln,
		upgrader: upgrader,
		conns:    make(chan Conn, queueCapacity),
		errors:   make(chan error, 1),
	}

	wsListener.server = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "websocket upgrade requires GET", http.StatusMethodNotAllowed)
			return
		}
		if !hasSubprotocol(r.Header.Get("Sec-WebSocket-Protocol"), "binary") {
			http.Error(w, "missing Sec-WebSocket-Protocol: binary", http.StatusBadRequest)
			return
		}
		select {
		case wsListener.admission <- struct{}{}:
			defer func() { <-wsListener.admission }()
		default:
			http.Error(w, "websocket listener is saturated", http.StatusServiceUnavailable)
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
	}),
		ReadHeaderTimeout: resolveWebSocketReadTimeout(t.ReadTimeout),
		IdleTimeout:       resolveWebSocketIdleTimeout(t.IdleTimeout),
		MaxHeaderBytes:    resolveWebSocketMaxHeaderBytes(t.MaxHeaderBytes),
	}

	wsListener.ctx, wsListener.cancel = context.WithCancel(context.Background())
	wsListener.admission = make(chan struct{}, queueCapacity)
	go func() {
		if err := wsListener.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			wsListener.errors <- err
		}
	}()

	return wsListener, nil
}

func (t *WebSocketTransport) validateOriginPolicy() error {
	for _, origin := range t.OriginPolicy.AllowedOrigins {
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
			parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("transport: invalid WebSocket allowed origin %q", origin)
		}
	}
	return nil
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
	listener  net.Listener
	server    *http.Server
	upgrader  websocket.Upgrader
	conns     chan Conn
	errors    chan error
	ctx       context.Context
	cancel    context.CancelFunc
	admission chan struct{}
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
	serverErr := l.server.Close()
	listenerErr := l.listener.Close()
	if errors.Is(serverErr, net.ErrClosed) {
		serverErr = nil
	}
	if errors.Is(listenerErr, net.ErrClosed) {
		listenerErr = nil
	}
	return errors.Join(serverErr, listenerErr)
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

func (c *wsConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

func (c *wsConn) SetWriteDeadline(t time.Time) error {
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

func (t *WebSocketTransport) originChecker() func(*http.Request) bool {
	policy := t.OriginPolicy
	if policy.AllowAny {
		return func(_ *http.Request) bool { return true }
	}
	allowed := make(map[string]struct{}, len(policy.AllowedOrigins))
	for _, origin := range policy.AllowedOrigins {
		if canonical := canonicalOrigin(origin); canonical != "" {
			allowed[canonical] = struct{}{}
		}
	}
	allowMissing := policy.AllowMissing || len(allowed) == 0
	return func(r *http.Request) bool {
		origin := canonicalOrigin(r.Header.Get("Origin"))
		if origin == "" {
			return allowMissing
		}
		if len(allowed) != 0 {
			_, ok := allowed[origin]
			return ok
		}
		return sameOrigin(origin, r)
	}
}

func (t *WebSocketTransport) upgradeOriginChecker() func(*http.Request) bool {
	policyCheckOrigin := t.originChecker()
	customCheckOrigin := t.Upgrader.CheckOrigin
	return func(r *http.Request) bool {
		return policyCheckOrigin(r) && (customCheckOrigin == nil || customCheckOrigin(r))
	}
}

func canonicalOrigin(origin string) string {
	if origin == "" {
		return ""
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme + "://" + parsed.Host)
}

func sameOrigin(origin string, r *http.Request) bool {
	if r == nil {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return origin == strings.ToLower(scheme+"://"+r.Host)
}

func resolveWebSocketReadTimeout(value time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return defaultWebSocketReadTimeout
}

func resolveWebSocketIdleTimeout(value time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return defaultWebSocketIdleTimeout
}

func resolveWebSocketMaxHeaderBytes(value int) int {
	if value > 0 {
		return value
	}
	return defaultWebSocketMaxHeaderSize
}
