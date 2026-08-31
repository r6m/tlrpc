// Package tlrpc provides a schema-first TL RPC framework for Go servers and
// clients. Applications supply their own TL schema and service implementations.
package tlrpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sync"
	"time"

	"github.com/r6m/tlrpc/crypto"
	handshakev2 "github.com/r6m/tlrpc/internal/handshake"
	runtimev2 "github.com/r6m/tlrpc/internal/runtime"
	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
)

const (
	DefaultReliabilitySessionCapacity = 4096
	DefaultReliabilityMessageCapacity = 4096
	DefaultReliabilityTTL             = 10 * time.Minute
	DefaultMaxPayloadBytes            = 16 << 20
	DefaultMaxInFlightRequests        = 1024
	DefaultMaxConnections             = 4096
	DefaultMaxConnectionsPerIP        = 64
	DefaultMaxConnectionsPerAuthKey   = 4
	DefaultReadTimeout                = 2 * time.Minute
	DefaultWriteTimeout               = 30 * time.Second
)

// Server represents an RPC server
type Server struct {
	authKeys                   crypto.AuthKeyManager
	serverKeys                 crypto.ServerKeyManager
	store                      session.Store
	runtimeLeases              *runtimev2.SessionLeaseRegistry
	runtimeReliability         *runtimev2.ReliabilityRegistry
	runtimeHandshake           *handshakev2.Engine
	runtimePushes              *runtimePushRegistry
	dispatcher                 *dispatcher
	schemaLayer                int
	schemaLayerSet             bool
	unaryInterceptors          []UnaryInterceptor
	logger                     Logger
	services                   map[string]*serviceInfo
	shutdownCh                 chan struct{}
	observer                   Observer
	observerSink               *observerSink
	reliabilitySessions        int
	reliabilityMessages        int
	reliabilityTTL             time.Duration
	maxPayloadBytes            int
	maxInFlightRequests        int
	decodeLimits               mtproto.DecodeLimits
	maxEncodedResponseBytes    int
	physicalWriteQueueCapacity int
	maxConnections             int
	maxConnectionsPerIP        int
	maxConnectionsPerAuthKey   int
	maxSessionsPerConnection   int
	readTimeout                time.Duration
	writeTimeout               time.Duration
	shutdownGracePeriod        time.Duration
	handlerSlots               chan struct{}
	activeHandlers             int
	lifecycleMu                sync.Mutex
	listeners                  map[*ownedListener]struct{}
	connections                map[*ownedConn]struct{}
	connectionStates           map[*ownedConn]*connectionState
	connectionByID             map[uint64]*connectionState
	activeConnectionsByIP      map[string]int
	activeConnectionsByAuth    map[int64]int
	activeSessions             int
	pendingSessionAcquire      map[session.SessionKey]bool
	nextConnectionID           uint64
	connectionWG               sync.WaitGroup
	stopOnce                   sync.Once
	stopDone                   chan struct{}
}

type closer interface {
	Close() error
}

type ownedListener struct {
	closer closer
	once   sync.Once
	err    error
}

func (l *ownedListener) close() error {
	l.once.Do(func() {
		l.err = l.closer.Close()
	})
	return l.err
}

type ownedConn struct {
	conn transport.Conn
	once sync.Once
	err  error
}

func (c *ownedConn) close() error {
	c.once.Do(func() {
		c.err = c.conn.Close()
	})
	return c.err
}

// serviceInfo stores service implementation info
type serviceInfo struct {
	desc ServiceDesc
	impl interface{}
}

// NewServer creates a new RPC server with the given options
func NewServer(opts ...ServerOption) *Server {
	s := &Server{
		dispatcher:                 newDispatcher(),
		authKeys:                   crypto.NewMemoryAuthKeyManager(),
		serverKeys:                 crypto.NewMemoryServerKeyManager(),
		store:                      session.NewMemoryStore(),
		services:                   make(map[string]*serviceInfo),
		shutdownCh:                 make(chan struct{}),
		stopDone:                   make(chan struct{}),
		listeners:                  make(map[*ownedListener]struct{}),
		connections:                make(map[*ownedConn]struct{}),
		connectionStates:           make(map[*ownedConn]*connectionState),
		connectionByID:             make(map[uint64]*connectionState),
		activeConnectionsByIP:      make(map[string]int),
		activeConnectionsByAuth:    make(map[int64]int),
		reliabilitySessions:        DefaultReliabilitySessionCapacity,
		reliabilityMessages:        DefaultReliabilityMessageCapacity,
		reliabilityTTL:             DefaultReliabilityTTL,
		maxPayloadBytes:            DefaultMaxPayloadBytes,
		maxInFlightRequests:        DefaultMaxInFlightRequests,
		maxConnections:             DefaultMaxConnections,
		maxConnectionsPerIP:        DefaultMaxConnectionsPerIP,
		maxConnectionsPerAuthKey:   DefaultMaxConnectionsPerAuthKey,
		decodeLimits:               mtproto.DecodeLimits{},
		maxEncodedResponseBytes:    DefaultMaxEncodedTLBytes,
		physicalWriteQueueCapacity: runtimev2.DefaultPhysicalWriteQueueCapacity,
		maxSessionsPerConnection:   runtimev2.DefaultConnectionSessionCapacity,
		readTimeout:                DefaultReadTimeout,
		writeTimeout:               DefaultWriteTimeout,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.setupObserver()
	s.runtimePushes = newRuntimePushRegistry(s)
	if s.maxInFlightRequests > 0 {
		s.handlerSlots = make(chan struct{}, s.maxInFlightRequests)
	}
	s.runtimeLeases = runtimev2.NewSessionLeaseRegistry(s.store)
	var err error
	s.runtimeReliability, err = runtimev2.NewReliabilityRegistry(runtimev2.ReliabilityRegistryConfig{
		MaxSessions: s.reliabilitySessions, MessageCapacity: s.reliabilityMessages, TTL: s.reliabilityTTL,
	})
	if err != nil {
		panic(err)
	}
	s.runtimeHandshake, err = handshakev2.New(handshakev2.Config{
		AuthKeys: s.authKeys, ServerKeys: s.serverKeys,
	})
	if err != nil {
		panic(err)
	}
	return s
}

// RegisterService registers a service implementation with the server.
// This is called by generated Register*Server functions.
// Panics on registration errors (gRPC-style).
func (s *Server) RegisterService(desc ServiceDesc, impl interface{}) {
	if desc.ServiceName == "" {
		panic("service name is required")
	}
	if desc.HandlerType == nil {
		panic(fmt.Sprintf("service %q handler type is required", desc.ServiceName))
	}
	if len(desc.Methods) == 0 {
		panic(fmt.Sprintf("service %q must declare at least one method", desc.ServiceName))
	}
	if impl == nil {
		panic("implementation cannot be nil")
	}
	if _, exists := s.services[desc.ServiceName]; exists {
		panic(fmt.Sprintf("service %s already registered", desc.ServiceName))
	}
	if s.schemaLayerSet && s.schemaLayer != desc.SchemaLayer {
		panic(fmt.Sprintf("service %q schema layer %d conflicts with server schema layer %d", desc.ServiceName, desc.SchemaLayer, s.schemaLayer))
	}

	handlerType := reflect.TypeOf(desc.HandlerType)
	if handlerType.Kind() != reflect.Ptr || handlerType.Elem().Kind() != reflect.Interface {
		panic(fmt.Sprintf("service %q handler type must be a pointer to an interface", desc.ServiceName))
	}
	implType := reflect.TypeOf(impl)
	if !implType.Implements(handlerType.Elem()) {
		panic(fmt.Sprintf("implementation %T does not satisfy handler type %v", impl, handlerType.Elem()))
	}

	type registration struct {
		method  MethodDesc
		invoker func(context.Context, TLObject) (interface{}, error)
	}
	registrations := make([]registration, 0, len(desc.Methods))
	methodIDs := make(map[uint32]string, len(desc.Methods))
	methodNames := make(map[string]struct{}, len(desc.Methods))
	for _, method := range desc.Methods {
		if method.MethodName == "" {
			panic(fmt.Sprintf("service %q has a method with no name", desc.ServiceName))
		}
		if _, exists := methodNames[method.MethodName]; exists {
			panic(fmt.Sprintf("service %q has duplicate method name %q", desc.ServiceName, method.MethodName))
		}
		methodNames[method.MethodName] = struct{}{}
		if method.ConstructorID == 0 {
			panic(fmt.Sprintf("method %q is missing constructor ID", method.MethodName))
		}
		if method.NewRequest == nil {
			panic(fmt.Sprintf("method %q is missing request constructor", method.MethodName))
		}
		if method.Handler == nil {
			panic(fmt.Sprintf("method %q is missing handler", method.MethodName))
		}
		if previous, exists := methodIDs[method.ConstructorID]; exists {
			panic(fmt.Sprintf("duplicate method constructor ID 0x%08x for %q and %q", method.ConstructorID, previous, method.MethodName))
		}
		methodIDs[method.ConstructorID] = method.MethodName

		invoker, err := bindServiceMethodHandler(impl, method.Handler)
		if err != nil {
			panic(fmt.Sprintf("method %q handler bind failed: %v", method.MethodName, err))
		}
		request := method.NewRequest()
		if request == nil {
			panic(fmt.Sprintf("method %q request constructor returned nil", method.MethodName))
		}
		if request.ConstructorID() != method.ConstructorID {
			panic(fmt.Sprintf("method %q request constructor ID 0x%08x does not match descriptor ID 0x%08x", method.MethodName, request.ConstructorID(), method.ConstructorID))
		}
		if _, exists := s.dispatcher.LookupMethod(method.ConstructorID); exists {
			panic(fmt.Sprintf("duplicate method constructor ID 0x%08x for %q", method.ConstructorID, method.MethodName))
		}
		if _, exists := s.dispatcher.LookupConstructor(method.ConstructorID); exists {
			panic(fmt.Sprintf("duplicate request constructor ID 0x%08x for %q", method.ConstructorID, method.MethodName))
		}

		registrations = append(registrations, registration{method: method, invoker: invoker})
	}

	for _, registration := range registrations {
		s.dispatcher.RegisterConstructor(registration.method.ConstructorID, registration.method.NewRequest)
		s.dispatcher.RegisterMethod(registration.method.ConstructorID, registration.invoker)
	}

	s.schemaLayer = desc.SchemaLayer
	s.schemaLayerSet = true
	s.services[desc.ServiceName] = &serviceInfo{desc: desc, impl: impl}
}

func bindServiceMethodHandler(impl interface{}, handler interface{}) (func(context.Context, TLObject) (interface{}, error), error) {
	handlerValue := reflect.ValueOf(handler)
	if !handlerValue.IsValid() || handlerValue.Kind() != reflect.Func {
		return nil, errors.New("handler is not a function")
	}

	handlerType := handlerValue.Type()
	if handlerType.NumIn() != 3 || handlerType.NumOut() != 2 {
		return nil, fmt.Errorf("unexpected handler signature: %v", handlerType)
	}
	if !handlerType.In(1).Implements(reflect.TypeOf((*context.Context)(nil)).Elem()) {
		return nil, fmt.Errorf("second arg must implement context.Context: %v", handlerType)
	}
	errType := reflect.TypeOf((*error)(nil)).Elem()
	if !handlerType.Out(1).Implements(errType) {
		return nil, fmt.Errorf("second return value must be error: %v", handlerType)
	}

	implValue := reflect.ValueOf(impl)
	reqType := handlerType.In(2)

	return func(ctx context.Context, req TLObject) (interface{}, error) {
		reqValue := reflect.ValueOf(req)
		if !reqValue.IsValid() {
			reqValue = reflect.Zero(reqType)
		} else if reqType.Kind() == reflect.Interface {
			if !reqValue.Type().Implements(reqType) {
				return nil, fmt.Errorf("request type %T does not implement %v", req, reqType)
			}
		} else if !reqValue.Type().AssignableTo(reqType) {
			return nil, fmt.Errorf("request type %T is not assignable to %v", req, reqType)
		}

		out := handlerValue.Call([]reflect.Value{implValue, reflect.ValueOf(ctx), reqValue})
		resp := out[0].Interface()
		if out[1].IsNil() {
			return resp, nil
		}
		return resp, out[1].Interface().(error)
	}, nil
}

// Serve starts serving on the given listener
func (s *Server) Serve(lis net.Listener) error {
	owned, ok := s.registerListener(lis)
	if !ok {
		return nil
	}
	defer s.unregisterListener(owned)

	for {
		conn, err := lis.Accept()
		if err != nil {
			if s.stopped() {
				return nil
			}
			return err
		}
		controlled := s.controlConn(
			transport.NewMTProtoConn(conn, transport.NegotiatorConfig{AllowObfuscation: true}),
		)
		if !s.serveConn(controlled) {
			return nil
		}
	}
}

// ServeTransport starts serving on a transport.Listener.
func (s *Server) ServeTransport(lis transport.Listener) error {
	owned, ok := s.registerListener(lis)
	if !ok {
		return nil
	}
	defer s.unregisterListener(owned)

	for {
		conn, err := lis.Accept()
		if err != nil {
			if s.stopped() {
				return nil
			}
			return err
		}
		if !s.serveConn(s.controlConn(conn)) {
			return nil
		}
	}
}

// Stop stops the server
func (s *Server) Stop() error {
	s.stopOnce.Do(func() {
		s.lifecycleMu.Lock()
		close(s.shutdownCh)
		listeners := make([]*ownedListener, 0, len(s.listeners))
		for lis := range s.listeners {
			listeners = append(listeners, lis)
		}
		connections := make([]*ownedConn, 0, len(s.connections))
		for conn := range s.connections {
			connections = append(connections, conn)
		}
		s.lifecycleMu.Unlock()

		for _, lis := range listeners {
			_ = lis.close()
		}
		if s.shutdownGracePeriod > 0 {
			done := make(chan struct{})
			go func() {
				s.connectionWG.Wait()
				close(done)
			}()
			timer := time.NewTimer(s.shutdownGracePeriod)
			select {
			case <-done:
				timer.Stop()
				s.closeObserver()
				close(s.stopDone)
				return
			case <-timer.C:
			}
		}
		for _, conn := range connections {
			_ = conn.close()
		}
		s.connectionWG.Wait()
		s.closeObserver()
		close(s.stopDone)
	})
	<-s.stopDone
	return nil
}

func (s *Server) registerListener(lis closer) (*ownedListener, bool) {
	owned := &ownedListener{closer: lis}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.stoppedLocked() {
		return nil, false
	}
	s.listeners[owned] = struct{}{}
	return owned, true
}

func (s *Server) unregisterListener(lis *ownedListener) {
	s.lifecycleMu.Lock()
	delete(s.listeners, lis)
	s.lifecycleMu.Unlock()
}

func (s *Server) serveConn(conn transport.Conn) bool {
	owned := &ownedConn{conn: conn}
	s.lifecycleMu.Lock()
	if s.stoppedLocked() {
		s.lifecycleMu.Unlock()
		_ = owned.close()
		return false
	}
	s.connections[owned] = struct{}{}
	s.connectionWG.Add(1)
	s.lifecycleMu.Unlock()
	state, ok := s.admitConnection(owned, conn)
	if !ok {
		_ = owned.close()
		s.lifecycleMu.Lock()
		delete(s.connections, owned)
		delete(s.connectionStates, owned)
		s.lifecycleMu.Unlock()
		s.connectionWG.Done()
		return true
	}
	application := newRuntimeApplicationDispatcher(s)
	runtimeConn, err := runtimev2.NewConnection(runtimev2.ConnectionConfig{
		ConnectionID: state.id, Conn: conn, AuthKeys: s.authKeys, Handshake: s.runtimeHandshake,
		Leases: s.runtimeLeases, Reliability: s.runtimeReliability,
		Application: application, MaxPayloadBytes: s.maxPayloadBytes,
		MaxDecodedPayload: s.maxPayloadBytes, ActiveRequests: s.maxInFlightRequests,
		DecodeLimits: s.decodeLimits, MaxEncodedBytes: s.maxEncodedResponseBytes,
		FrameSinkPolicy: runtimev2.FrameSinkPolicy{
			QueueCapacity: s.physicalWriteQueueCapacity, WriteTimeout: s.writeTimeout,
			Observe: func(bytes int, outcome string, err error, duration time.Duration) {
				s.emitWriterEvent(state, bytes, outcome, classifyWriterError(err), duration)
			},
		},
		SessionCapacity: s.maxSessionsPerConnection,
		SchemaLayer:     s.schemaLayer, Transport: runtimeTransportMode(conn),
		Presence: s.runtimePushes,
	})
	if err != nil || application.setupErr != nil {
		reason := "setup_failed"
		s.finishConnection(owned, state, reason)
		s.lifecycleMu.Lock()
		delete(s.connections, owned)
		s.lifecycleMu.Unlock()
		s.connectionWG.Done()
		_ = owned.close()
		return false
	}

	go func() {
		reason := "closed"
		defer func() {
			_ = owned.close()
			s.finishConnection(owned, state, reason)
			s.lifecycleMu.Lock()
			delete(s.connections, owned)
			s.lifecycleMu.Unlock()
			s.connectionWG.Done()
		}()
		if runErr := runtimeConn.Run(context.Background()); runErr != nil {
			reason = classifyConnectionClose(runErr, s.stopped())
			if !s.stopped() && s.logger != nil {
				s.logger.Error("connection runtime stopped", "error", runErr)
			}
		} else if s.stopped() {
			reason = "shutdown"
		}
	}()
	return true
}

func runtimeTransportMode(conn transport.Conn) string {
	if provider, ok := conn.(interface{ TransportMode() string }); ok {
		return provider.TransportMode()
	}
	return ""
}

func (s *Server) stopped() bool {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	return s.stoppedLocked()
}

func (s *Server) stoppedLocked() bool {
	select {
	case <-s.shutdownCh:
		return true
	default:
		return false
	}
}

// ServerOption represents server configuration options
type ServerOption func(*Server)

// WithUnaryInterceptor adds a unary interceptor to the server (gRPC-like).
func WithUnaryInterceptor(i UnaryInterceptor) ServerOption {
	return func(s *Server) {
		s.unaryInterceptors = append(s.unaryInterceptors, i)
	}
}

// WithSessionStore sets the detached durable Runtime v2 session store.
func WithSessionStore(store session.Store) ServerOption {
	return func(s *Server) {
		if store == nil {
			panic("tlrpc: session store is required")
		}
		s.store = store
	}
}

// WithLogger sets the logger for the server
func WithLogger(l Logger) ServerOption {
	return func(s *Server) {
		s.logger = l
	}
}

// WithAuthKeyManager sets the auth key manager.
func WithAuthKeyManager(manager crypto.AuthKeyManager) ServerOption {
	return func(s *Server) {
		if manager != nil {
			s.authKeys = manager
		}
	}
}

// WithServerKeyManager sets the server key manager.
func WithServerKeyManager(manager crypto.ServerKeyManager) ServerOption {
	return func(s *Server) {
		if manager != nil {
			s.serverKeys = manager
		}
	}
}

// WithReliabilityLimits configures strict server-wide session and per-session
// message bounds for MTProto acknowledgement, state, and resend tracking.
// All values must be positive; invalid configuration panics during setup.
func WithReliabilityLimits(sessionCapacity, messageCapacity int, ttl time.Duration) ServerOption {
	if sessionCapacity <= 0 {
		panic("tlrpc: reliability session capacity must be positive")
	}
	if messageCapacity <= 0 {
		panic("tlrpc: reliability message capacity must be positive")
	}
	if ttl <= 0 {
		panic("tlrpc: reliability TTL must be positive")
	}
	return func(s *Server) {
		s.reliabilitySessions = sessionCapacity
		s.reliabilityMessages = messageCapacity
		s.reliabilityTTL = ttl
	}
}

// ResourceLimits configures the bounded Runtime v2 connection and application
// execution policy. Zero selects framework defaults for decode, response, and
// writer bounds; zero connection quotas are unlimited; zero shutdown grace
// closes active connections immediately. Negative values are invalid.
type ResourceLimits struct {
	MaxPayloadBytes            int
	MaxInFlightRequests        int
	MaxDecodedBytes            int64
	MaxWrappers                int
	MaxContainers              int
	MaxVectorElements          int64
	MaxObjectNodes             int64
	MaxObjectDepth             int
	MaxGzipExpansionRatio      int64
	MaxGzipWorkBytes           int64
	MaxEncodedResponseBytes    int
	PhysicalWriteQueueCapacity int
	MaxConnections             int
	MaxConnectionsPerIP        int
	MaxConnectionsPerAuthKey   int
	MaxSessionsPerConnection   int
	ReadTimeout                time.Duration
	WriteTimeout               time.Duration
	ShutdownGracePeriod        time.Duration
}

// WithResourceLimits applies one TL-native resource policy to Runtime v2.
func WithResourceLimits(limits ResourceLimits) ServerOption {
	return func(s *Server) {
		if limits.MaxPayloadBytes <= 0 {
			panic("tlrpc: maximum payload bytes must be positive")
		}
		if limits.MaxInFlightRequests <= 0 {
			panic("tlrpc: maximum in-flight requests must be positive")
		}
		decodeLimits := mtproto.DecodeLimits{
			MaxDecodedBytes: limits.MaxDecodedBytes, MaxWrappers: limits.MaxWrappers,
			MaxContainers: limits.MaxContainers, MaxVectorElements: limits.MaxVectorElements,
			MaxObjectNodes: limits.MaxObjectNodes, MaxObjectDepth: limits.MaxObjectDepth,
			MaxGzipRatio: limits.MaxGzipExpansionRatio, MaxGzipWorkBytes: limits.MaxGzipWorkBytes,
		}
		if _, err := mtproto.NewDecodeBudget(decodeLimits); err != nil {
			panic(err)
		}
		encodedLimit := limits.MaxEncodedResponseBytes
		if encodedLimit < 0 {
			panic("tlrpc: maximum encoded response bytes cannot be negative")
		}
		if encodedLimit == 0 {
			encodedLimit = DefaultMaxEncodedTLBytes
		}
		queueCapacity := limits.PhysicalWriteQueueCapacity
		if queueCapacity < 0 {
			panic("tlrpc: physical write queue capacity cannot be negative")
		}
		if queueCapacity == 0 {
			queueCapacity = runtimev2.DefaultPhysicalWriteQueueCapacity
		}
		if limits.MaxConnections < 0 {
			panic("tlrpc: maximum connections cannot be negative")
		}
		if limits.MaxConnectionsPerIP < 0 {
			panic("tlrpc: maximum connections per IP cannot be negative")
		}
		if limits.MaxConnectionsPerAuthKey < 0 {
			panic("tlrpc: maximum connections per auth key cannot be negative")
		}
		if limits.MaxSessionsPerConnection < 0 {
			panic("tlrpc: maximum sessions per connection cannot be negative")
		}
		if limits.ReadTimeout <= 0 {
			panic("tlrpc: read timeout must be positive")
		}
		if limits.WriteTimeout <= 0 {
			panic("tlrpc: write timeout must be positive")
		}
		if limits.ShutdownGracePeriod < 0 {
			panic("tlrpc: shutdown grace period cannot be negative")
		}
		s.maxPayloadBytes = limits.MaxPayloadBytes
		s.maxInFlightRequests = limits.MaxInFlightRequests
		s.decodeLimits = decodeLimits
		s.maxEncodedResponseBytes = encodedLimit
		s.physicalWriteQueueCapacity = queueCapacity
		s.maxConnections = limits.MaxConnections
		s.maxConnectionsPerIP = limits.MaxConnectionsPerIP
		s.maxConnectionsPerAuthKey = limits.MaxConnectionsPerAuthKey
		if limits.MaxSessionsPerConnection > 0 {
			s.maxSessionsPerConnection = limits.MaxSessionsPerConnection
		}
		s.readTimeout = limits.ReadTimeout
		s.writeTimeout = limits.WriteTimeout
		s.shutdownGracePeriod = limits.ShutdownGracePeriod
	}
}
