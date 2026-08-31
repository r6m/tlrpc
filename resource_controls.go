package tlrpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/r6m/tlrpc/crypto"
	runtimev2 "github.com/r6m/tlrpc/internal/runtime"
	"github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
)

var (
	// ErrMessageTooLarge reports a complete MTProto payload that exceeds the
	// configured inbound or outbound limit.
	ErrMessageTooLarge = errors.New("tlrpc: MTProto payload exceeds maximum message size")
	// ErrServerStopped is returned when shutdown cancels a handler waiting for a
	// server-wide application execution slot.
	ErrServerStopped = errors.New("tlrpc: server stopped")
)

type messageSizeError struct {
	direction string
	size      int
	limit     int
}

func (e *messageSizeError) Error() string {
	return fmt.Sprintf("%s: %s payload is %d bytes, limit is %d", ErrMessageTooLarge, e.direction, e.size, e.limit)
}

func (e *messageSizeError) Unwrap() error { return ErrMessageTooLarge }

// controlledConn is the single runtime I/O boundary for an accepted
// connection. It enforces complete-payload limits, directional operation
// deadlines. Runtime v2's connection frame sink is the sole serialized,
// bounded physical-write owner after the handshake.
type controlledConn struct {
	base            transport.Conn
	maxPayloadBytes int
	readTimeout     time.Duration
	writeTimeout    time.Duration
}

type connectionState struct {
	owner                      *ownedConn
	id                         uint64
	transport                  string
	remoteIP                   string
	acceptedAt                 time.Time
	authKeyID                  int64
	authCounted                bool
	handshakeObserved          bool
	closeReason                string
	activeConnections          int
	activeConnectionsForIP     int
	activeConnectionsForAuth   int
	activeSessionsOnConnection int
	activeSessions             map[session.SessionKey]bool
}

func (s *Server) controlConn(base transport.Conn) *controlledConn {
	c := &controlledConn{
		base:            base,
		maxPayloadBytes: s.maxPayloadBytes,
		readTimeout:     s.readTimeout,
		writeTimeout:    s.writeTimeout,
	}
	return c
}

func (c *controlledConn) ReadMessage(maxPayloadBytes int) ([]byte, error) {
	clear, err := c.beginRead()
	if err != nil {
		return nil, err
	}
	if c.maxPayloadBytes > 0 && (maxPayloadBytes <= 0 || c.maxPayloadBytes < maxPayloadBytes) {
		maxPayloadBytes = c.maxPayloadBytes
	}
	payload, readErr := c.base.ReadMessage(maxPayloadBytes)
	clearErr := clear()
	if readErr != nil {
		if errors.Is(readErr, transport.ErrPayloadTooLarge) {
			return nil, fmt.Errorf("%w: inbound payload exceeds limit %d", ErrMessageTooLarge, maxPayloadBytes)
		}
		return nil, readErr
	}
	if clearErr != nil {
		return nil, clearErr
	}
	return payload, nil
}

func (c *controlledConn) WriteMessage(payload []byte) error {
	if c.maxPayloadBytes > 0 && len(payload) > c.maxPayloadBytes {
		return &messageSizeError{direction: "outbound", size: len(payload), limit: c.maxPayloadBytes}
	}

	clear, err := c.beginWrite(time.Time{})
	if err != nil {
		return err
	}
	writeErr := c.base.WriteMessage(payload)
	clearErr := clear()
	if writeErr != nil {
		return writeErr
	}
	return clearErr
}

func (c *controlledConn) beginRead() (func() error, error) {
	if c.readTimeout <= 0 {
		return func() error { return nil }, nil
	}
	if err := c.base.SetReadDeadline(time.Now().Add(c.readTimeout)); err != nil {
		return nil, err
	}
	return func() error { return c.base.SetReadDeadline(time.Time{}) }, nil
}

func (c *controlledConn) beginWrite(deadline time.Time) (func() error, error) {
	if c.writeTimeout <= 0 {
		return func() error { return nil }, nil
	}
	if deadline.IsZero() {
		deadline = time.Now().Add(c.writeTimeout)
	}
	if err := c.base.SetWriteDeadline(deadline); err != nil {
		return nil, err
	}
	return func() error { return c.base.SetWriteDeadline(time.Time{}) }, nil
}

func (c *controlledConn) Close() error             { return c.base.Close() }
func (c *controlledConn) LocalAddr() net.Addr      { return c.base.LocalAddr() }
func (c *controlledConn) RemoteAddr() net.Addr     { return c.base.RemoteAddr() }
func (c *controlledConn) Context() context.Context { return c.base.Context() }
func (c *controlledConn) SetReadDeadline(t time.Time) error {
	return c.base.SetReadDeadline(t)
}
func (c *controlledConn) SetWriteDeadline(t time.Time) error {
	return c.base.SetWriteDeadline(t)
}

func (c *controlledConn) TransportMode() string {
	if provider, ok := c.base.(interface{ TransportMode() string }); ok {
		return provider.TransportMode()
	}
	return ""
}

func (s *Server) acquireHandler(ctx context.Context) error {
	if s.handlerSlots == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-s.shutdownCh:
		return ErrServerStopped
	default:
	}
	select {
	case s.handlerSlots <- struct{}{}:
		s.lifecycleMu.Lock()
		s.activeHandlers++
		active := s.activeHandlers
		s.lifecycleMu.Unlock()
		s.emitGauge("active_handlers", 0, "", 0, active)
		select {
		case <-s.shutdownCh:
			s.releaseHandler()
			s.emitAdmission("handler", "shutdown", nil, 0, cap(s.handlerSlots), active)
			return ErrServerStopped
		default:
			return nil
		}
	case <-ctx.Done():
		s.emitAdmission("handler", classifyAdmissionOutcome(ctx.Err()), nil, 0, cap(s.handlerSlots), len(s.handlerSlots))
		return ctx.Err()
	case <-s.shutdownCh:
		s.emitAdmission("handler", "shutdown", nil, 0, cap(s.handlerSlots), len(s.handlerSlots))
		return ErrServerStopped
	}
}

func (s *Server) releaseHandler() {
	if s.handlerSlots != nil {
		<-s.handlerSlots
		s.lifecycleMu.Lock()
		if s.activeHandlers > 0 {
			s.activeHandlers--
		}
		active := s.activeHandlers
		s.lifecycleMu.Unlock()
		s.emitGauge("active_handlers", 0, "", 0, active)
	}
}

func (s *Server) admitConnection(owned *ownedConn, conn transport.Conn) (*connectionState, bool) {
	state := &connectionState{
		owner:          owned,
		id:             s.nextConnID(),
		transport:      runtimeTransportMode(conn),
		remoteIP:       remoteIPKey(conn.RemoteAddr()),
		acceptedAt:     time.Now(),
		activeSessions: make(map[session.SessionKey]bool),
	}
	s.lifecycleMu.Lock()
	if s.maxConnections > 0 && len(s.connections) > s.maxConnections {
		active := len(s.connections) - 1
		s.lifecycleMu.Unlock()
		s.emitAdmission("connection_global", "rejected", state, 0, s.maxConnections, active)
		return state, false
	}
	if state.remoteIP != "" && s.maxConnectionsPerIP > 0 && s.activeConnectionsByIP[state.remoteIP] >= s.maxConnectionsPerIP {
		active := s.activeConnectionsByIP[state.remoteIP]
		s.lifecycleMu.Unlock()
		s.emitAdmission("connection_ip", "rejected", state, 0, s.maxConnectionsPerIP, active)
		return state, false
	}
	s.connectionStates[owned] = state
	s.connectionByID[state.id] = state
	if state.remoteIP != "" {
		s.activeConnectionsByIP[state.remoteIP]++
	}
	state.activeConnections = len(s.connections)
	state.activeConnectionsForIP = s.activeConnectionsByIP[state.remoteIP]
	s.lifecycleMu.Unlock()
	s.emitConnectionOpened(state)
	return state, true
}

func (s *Server) finishConnection(owned *ownedConn, state *connectionState, fallbackReason string) {
	if s == nil || state == nil {
		return
	}
	s.lifecycleMu.Lock()
	if s.connectionStates[owned] == nil {
		s.lifecycleMu.Unlock()
		return
	}
	delete(s.connectionStates, owned)
	delete(s.connectionByID, state.id)
	if state.remoteIP != "" {
		next := s.activeConnectionsByIP[state.remoteIP] - 1
		if next <= 0 {
			delete(s.activeConnectionsByIP, state.remoteIP)
			state.activeConnectionsForIP = 0
		} else {
			s.activeConnectionsByIP[state.remoteIP] = next
			state.activeConnectionsForIP = next
		}
	}
	if state.authCounted && state.authKeyID != 0 {
		next := s.activeConnectionsByAuth[state.authKeyID] - 1
		if next <= 0 {
			delete(s.activeConnectionsByAuth, state.authKeyID)
			next = 0
		} else {
			s.activeConnectionsByAuth[state.authKeyID] = next
		}
		state.activeConnectionsForAuth = next
	}
	for key := range state.activeSessions {
		delete(state.activeSessions, key)
		if s.activeSessions > 0 {
			s.activeSessions--
		}
	}
	state.activeSessionsOnConnection = 0
	state.activeConnections = len(s.connections) - 1
	reason := state.closeReason
	if reason == "" {
		reason = fallbackReason
	}
	s.lifecycleMu.Unlock()
	if !state.handshakeObserved && reason != "shutdown" {
		s.emitHandshake(state, "failed", reason, time.Since(state.acceptedAt))
	}
	s.emitConnectionClosed(state, reason)
}

func (s *Server) handleObservedSessionBound(binding Binding) {
	key := sessionKeyFromBinding(binding)
	source := s.consumeSessionAcquire(key)
	s.lifecycleMu.Lock()
	state := s.connectionByID[binding.ConnectionID]
	if state == nil {
		s.lifecycleMu.Unlock()
		s.emitSessionEvent(SessionEvent{Action: "bound", ConnectionID: binding.ConnectionID, AuthKeyID: binding.AuthKeyID, SessionID: binding.SessionID, Source: source}, binding.AuthKeyID)
		return
	}
	emitHandshake := !state.handshakeObserved
	state.handshakeObserved = true
	if !state.authCounted && binding.AuthKeyID != 0 {
		active := s.activeConnectionsByAuth[binding.AuthKeyID]
		if s.maxConnectionsPerAuthKey > 0 && active >= s.maxConnectionsPerAuthKey {
			state.closeReason = "auth_connection_limit"
			s.lifecycleMu.Unlock()
			s.emitAdmission("connection_auth_key", "rejected", state, binding.AuthKeyID, s.maxConnectionsPerAuthKey, active)
			_ = state.owner.close()
			return
		}
		s.activeConnectionsByAuth[binding.AuthKeyID] = active + 1
		state.authKeyID = binding.AuthKeyID
		state.authCounted = true
		state.activeConnectionsForAuth = active + 1
	}
	if !state.activeSessions[key] {
		state.activeSessions[key] = true
		state.activeSessionsOnConnection++
		s.activeSessions++
	}
	activeSessions := s.activeSessions
	activeOnConnection := state.activeSessionsOnConnection
	s.lifecycleMu.Unlock()
	if emitHandshake {
		s.emitHandshake(state, "succeeded", "session_bound", time.Since(state.acceptedAt))
	}
	s.emitSessionEvent(SessionEvent{
		Action: "bound", ConnectionID: binding.ConnectionID,
		AuthKeyID: binding.AuthKeyID, SessionID: binding.SessionID, Source: source,
		ActiveSessions: activeSessions, ActiveSessionsOnConnection: activeOnConnection,
	}, binding.AuthKeyID)
	s.emitGauge("active_connections_per_auth_key", state.id, state.remoteIP, binding.AuthKeyID, state.activeConnectionsForAuth)
}

func (s *Server) handleObservedSessionUnbound(binding Binding) {
	key := sessionKeyFromBinding(binding)
	s.lifecycleMu.Lock()
	state := s.connectionByID[binding.ConnectionID]
	activeOnConnection := 0
	if state != nil && state.activeSessions[key] {
		delete(state.activeSessions, key)
		if state.activeSessionsOnConnection > 0 {
			state.activeSessionsOnConnection--
		}
		if s.activeSessions > 0 {
			s.activeSessions--
		}
		activeOnConnection = state.activeSessionsOnConnection
	}
	activeSessions := s.activeSessions
	s.lifecycleMu.Unlock()
	s.emitSessionEvent(SessionEvent{
		Action: "released", ConnectionID: binding.ConnectionID,
		AuthKeyID: binding.AuthKeyID, SessionID: binding.SessionID, Source: "unknown",
		ActiveSessions: activeSessions, ActiveSessionsOnConnection: activeOnConnection,
	}, binding.AuthKeyID)
}

func (s *Server) nextConnID() uint64 {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.nextConnectionID++
	return s.nextConnectionID
}

func remoteIPKey(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err == nil {
		return host
	}
	return addr.String()
}

func classifyAdmissionOutcome(err error) string {
	switch {
	case err == nil:
		return "rejected"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	default:
		return "rejected"
	}
}

func classifyConnectionClose(err error, stopping bool) string {
	switch {
	case stopping:
		return "shutdown"
	case err == nil:
		return "closed"
	case errors.Is(err, runtimev2.ErrHandshakeAuthKeyMismatch):
		return "handshake_auth_key_mismatch"
	case errors.Is(err, runtimev2.ErrConnectionProtocol):
		return "protocol"
	case errors.Is(err, os.ErrDeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		var capacity *runtimev2.ConnectionSessionCapacityError
		if errors.As(err, &capacity) {
			return "session_capacity"
		}
		return "failed"
	}
}

func sessionKeyFromBinding(binding Binding) session.SessionKey {
	return session.SessionKey{
		AuthKeyID: crypto.KeyID(binding.AuthKeyID),
		SessionID: binding.SessionID,
	}
}

var _ transport.Conn = (*controlledConn)(nil)
