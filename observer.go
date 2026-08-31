package tlrpc

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/r6m/tlrpc/session"
)

const defaultObserverBuffer = 256

type Event interface{ isTLRPCEvent() }

type Observer interface {
	ObserveTLRPC(Event)
}

type ConnectionEvent struct {
	Time                        time.Time
	Kind                        string
	ConnectionID                uint64
	Transport                   string
	RemoteIP                    string
	AuthKeyID                   int64
	Reason                      string
	Duration                    time.Duration
	ActiveConnections           int
	ActiveConnectionsForIP      int
	ActiveConnectionsForAuthKey int
}

func (ConnectionEvent) isTLRPCEvent() {}

type HandshakeEvent struct {
	Time           time.Time
	ConnectionID   uint64
	Transport      string
	RemoteIP       string
	Outcome        string
	Classification string
	Duration       time.Duration
}

func (HandshakeEvent) isTLRPCEvent() {}

type SessionEvent struct {
	Time                       time.Time
	Action                     string
	ConnectionID               uint64
	AuthKeyID                  int64
	SessionID                  int64
	Source                     string
	ActiveSessions             int
	ActiveSessionsOnConnection int
}

func (SessionEvent) isTLRPCEvent() {}

type RPCEvent struct {
	Time        time.Time
	Method      string
	AuthKeyID   int64
	SessionID   int64
	Layer       int
	Duration    time.Duration
	ResultClass string
	ErrorCode   int
}

func (RPCEvent) isTLRPCEvent() {}

type AdmissionEvent struct {
	Time         time.Time
	Scope        string
	Outcome      string
	ConnectionID uint64
	RemoteIP     string
	AuthKeyID    int64
	Limit        int
	Active       int
}

func (AdmissionEvent) isTLRPCEvent() {}

type WriterEvent struct {
	Time           time.Time
	ConnectionID   uint64
	Transport      string
	Bytes          int
	Outcome        string
	Classification string
	Duration       time.Duration
}

func (WriterEvent) isTLRPCEvent() {}

type StoreEvent struct {
	Time           time.Time
	Operation      string
	AuthKeyID      int64
	SessionID      int64
	Duration       time.Duration
	Outcome        string
	Created        bool
	Classification string
}

func (StoreEvent) isTLRPCEvent() {}

type GaugeEvent struct {
	Time         time.Time
	Name         string
	ConnectionID uint64
	RemoteIP     string
	AuthKeyID    int64
	Value        int
}

func (GaugeEvent) isTLRPCEvent() {}

type observerSink struct {
	observer  Observer
	events    chan Event
	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once
}

func newObserverSink(observer Observer) *observerSink {
	if observer == nil {
		return nil
	}
	sink := &observerSink{
		observer: observer,
		events:   make(chan Event, defaultObserverBuffer),
	}
	go func() {
		for event := range sink.events {
			func() {
				defer func() { _ = recover() }()
				sink.observer.ObserveTLRPC(event)
			}()
		}
	}()
	return sink
}

func (s *observerSink) emit(event Event) {
	if s == nil || event == nil {
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return
	}
	select {
	case s.events <- event:
	default:
	}
}

func (s *observerSink) close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		close(s.events)
		s.mu.Unlock()
	})
}

func WithObserver(observer Observer) ServerOption {
	return func(s *Server) {
		s.observer = observer
	}
}

func (s *Server) setupObserver() {
	if s == nil || s.observer == nil {
		return
	}
	s.observerSink = newObserverSink(s.observer)
	s.store = &observedSessionStore{server: s, inner: s.store}
	s.unaryInterceptors = append([]UnaryInterceptor{s.observeRPC()}, s.unaryInterceptors...)
}

func (s *Server) observeRPC() UnaryInterceptor {
	return func(ctx context.Context, req interface{}, info *UnaryServerInfo, handler UnaryHandler) (interface{}, error) {
		started := time.Now()
		resp, err := handler(ctx, req)
		if s == nil || s.observerSink == nil {
			return resp, err
		}
		class, code := classifyRPCResult(err)
		s.observerSink.emit(RPCEvent{
			Time:        time.Now(),
			Method:      observerMethod(info),
			AuthKeyID:   AuthKeyIDFromContext(ctx),
			SessionID:   observerSessionID(ctx),
			Layer:       LayerFromContext(ctx),
			Duration:    time.Since(started),
			ResultClass: class,
			ErrorCode:   code,
		})
		return resp, err
	}
}

type observedSessionStore struct {
	server *Server
	inner  session.Store
}

func (s *observedSessionStore) Load(ctx context.Context, key session.SessionKey) (session.Snapshot, error) {
	started := time.Now()
	snapshot, err := s.inner.Load(ctx, key)
	s.observe("load", key, time.Since(started), false, err)
	return snapshot, err
}

func (s *observedSessionStore) LoadOrCreate(ctx context.Context, key session.SessionKey, initial session.Snapshot) (session.Snapshot, bool, error) {
	started := time.Now()
	snapshot, created, err := s.inner.LoadOrCreate(ctx, key, initial)
	s.observe("load_or_create", key, time.Since(started), created, err)
	if err == nil && s.server != nil {
		s.server.noteSessionAcquire(key, created)
	}
	return snapshot, created, err
}

func (s *observedSessionStore) Save(ctx context.Context, key session.SessionKey, snapshot session.Snapshot) error {
	started := time.Now()
	err := s.inner.Save(ctx, key, snapshot)
	s.observe("save", key, time.Since(started), false, err)
	return err
}

func (s *observedSessionStore) Delete(ctx context.Context, key session.SessionKey) error {
	started := time.Now()
	err := s.inner.Delete(ctx, key)
	s.observe("delete", key, time.Since(started), false, err)
	return err
}

func (s *observedSessionStore) observe(operation string, key session.SessionKey, duration time.Duration, created bool, err error) {
	if s == nil || s.server == nil || s.server.observerSink == nil {
		return
	}
	outcome := "ok"
	if err != nil {
		outcome = "failed"
	}
	s.server.observerSink.emit(StoreEvent{
		Time:           time.Now(),
		Operation:      operation,
		AuthKeyID:      int64(key.AuthKeyID),
		SessionID:      key.SessionID,
		Duration:       duration,
		Outcome:        outcome,
		Created:        created,
		Classification: classifyStoreError(err),
	})
}

func (s *Server) emitConnectionOpened(state *connectionState) {
	if s == nil || s.observerSink == nil || state == nil {
		return
	}
	s.observerSink.emit(ConnectionEvent{
		Time:                   time.Now(),
		Kind:                   "opened",
		ConnectionID:           state.id,
		Transport:              state.transport,
		RemoteIP:               state.remoteIP,
		ActiveConnections:      state.activeConnections,
		ActiveConnectionsForIP: state.activeConnectionsForIP,
	})
	s.emitGauge("active_connections", state.id, state.remoteIP, 0, state.activeConnections)
	s.emitGauge("active_connections_per_ip", state.id, state.remoteIP, 0, state.activeConnectionsForIP)
}

func (s *Server) emitConnectionClosed(state *connectionState, reason string) {
	if s == nil || s.observerSink == nil || state == nil {
		return
	}
	s.observerSink.emit(ConnectionEvent{
		Time:                        time.Now(),
		Kind:                        "closed",
		ConnectionID:                state.id,
		Transport:                   state.transport,
		RemoteIP:                    state.remoteIP,
		AuthKeyID:                   state.authKeyID,
		Reason:                      reason,
		Duration:                    time.Since(state.acceptedAt),
		ActiveConnections:           state.activeConnections,
		ActiveConnectionsForIP:      state.activeConnectionsForIP,
		ActiveConnectionsForAuthKey: state.activeConnectionsForAuth,
	})
	s.emitGauge("active_connections", state.id, state.remoteIP, state.authKeyID, state.activeConnections)
	s.emitGauge("active_connections_per_ip", state.id, state.remoteIP, state.authKeyID, state.activeConnectionsForIP)
	if state.authKeyID != 0 {
		s.emitGauge("active_connections_per_auth_key", state.id, state.remoteIP, state.authKeyID, state.activeConnectionsForAuth)
	}
}

func (s *Server) emitHandshake(state *connectionState, outcome, classification string, duration time.Duration) {
	if s == nil || s.observerSink == nil || state == nil {
		return
	}
	s.observerSink.emit(HandshakeEvent{
		Time: time.Now(), ConnectionID: state.id, Transport: state.transport,
		RemoteIP: state.remoteIP, Outcome: outcome, Classification: classification,
		Duration: duration,
	})
}

func (s *Server) emitSessionEvent(event SessionEvent, authKeyID int64) {
	if s == nil || s.observerSink == nil {
		return
	}
	event.Time = time.Now()
	s.observerSink.emit(event)
}

func (s *Server) emitAdmission(scope, outcome string, state *connectionState, authKeyID int64, limit, active int) {
	if s == nil || s.observerSink == nil {
		return
	}
	event := AdmissionEvent{
		Time:      time.Now(),
		Scope:     scope,
		Outcome:   outcome,
		AuthKeyID: authKeyID,
		Limit:     limit,
		Active:    active,
	}
	if state != nil {
		event.ConnectionID = state.id
		event.RemoteIP = state.remoteIP
		if authKeyID == 0 {
			event.AuthKeyID = state.authKeyID
		}
	}
	s.observerSink.emit(event)
}

func (s *Server) emitWriterEvent(state *connectionState, bytes int, outcome, classification string, duration time.Duration) {
	if s == nil || s.observerSink == nil {
		return
	}
	event := WriterEvent{
		Time:           time.Now(),
		Bytes:          bytes,
		Outcome:        outcome,
		Classification: classification,
		Duration:       duration,
	}
	if state != nil {
		event.ConnectionID = state.id
		event.Transport = state.transport
	}
	s.observerSink.emit(event)
}

func (s *Server) emitGauge(name string, connectionID uint64, remoteIP string, authKeyID int64, value int) {
	if s == nil || s.observerSink == nil {
		return
	}
	s.observerSink.emit(GaugeEvent{
		Time:         time.Now(),
		Name:         name,
		ConnectionID: connectionID,
		RemoteIP:     remoteIP,
		AuthKeyID:    authKeyID,
		Value:        value,
	})
}

func (s *Server) noteSessionAcquire(key session.SessionKey, created bool) {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	if s.pendingSessionAcquire == nil {
		s.pendingSessionAcquire = make(map[session.SessionKey]bool)
	}
	s.pendingSessionAcquire[key] = created
	s.lifecycleMu.Unlock()
}

func (s *Server) consumeSessionAcquire(key session.SessionKey) string {
	if s == nil {
		return "unknown"
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	created, ok := s.pendingSessionAcquire[key]
	if ok {
		delete(s.pendingSessionAcquire, key)
	}
	if !ok {
		return "unknown"
	}
	if created {
		return "created"
	}
	return "resumed"
}

func classifyRPCResult(err error) (string, int) {
	if err == nil {
		return "ok", 0
	}
	if errors.Is(err, context.Canceled) {
		return "canceled", 0
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded", 0
	}
	rpcErr := FromError(err)
	code := int(rpcErr.ErrorCode)
	switch rpcErr.ErrorCode {
	case 400:
		return "bad_request", code
	case 401:
		return "unauthorized", code
	case 403:
		return "forbidden", code
	case 404:
		return "not_found", code
	case 409:
		return "conflict", code
	case 429:
		return "rate_limited", code
	case 500:
		return "internal", code
	default:
		return "rpc_error", code
	}
}

func classifyStoreError(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, session.ErrSessionNotFound):
		return "not_found"
	case errors.Is(err, session.ErrSessionKeyMismatch):
		return "key_mismatch"
	default:
		return "failed"
	}
}

func classifyWriterError(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, os.ErrDeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "failed"
	}
}

func observerMethod(info *UnaryServerInfo) string {
	if info == nil {
		return ""
	}
	return info.FullMethod
}

func observerSessionID(ctx context.Context) int64 {
	binding, ok := BindingFromContext(ctx)
	if !ok {
		return 0
	}
	return binding.SessionID
}

func (s *Server) closeObserver() {
	if s == nil || s.observerSink == nil {
		return
	}
	s.observerSink.close()
}

var _ session.Store = (*observedSessionStore)(nil)
