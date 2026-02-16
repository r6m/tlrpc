// Package tlrpc provides the core framework for Telegram RPC servers and clients.
package tlrpc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
)

// Legacy error variables - moved to error.go

// Server represents an RPC server
type Server struct {
	transport          Transport
	authKeys           crypto.AuthKeyManager
	serverKeys         crypto.ServerKeyManager
	sessions           session.Manager
	dispatcher         *dispatcher
	maxLayer           int
	layers             []int
	unaryInterceptors  []UnaryInterceptor // New gRPC-like interceptors
	legacyInterceptors []Interceptor      // Legacy support
	logger             Logger
	services           map[string]*serviceInfo // for backward compatibility
	handshakeHandler   HandshakeHandler
	shutdownCh         chan struct{}
}

// serviceInfo stores service implementation info
type serviceInfo struct {
	desc ServiceDesc
	impl interface{}
}

// NewServer creates a new RPC server with the given options
func NewServer(opts ...ServerOption) *Server {
	s := &Server{
		dispatcher: newDispatcher(),
		authKeys:   crypto.NewMemoryAuthKeyManager(),
		serverKeys: crypto.NewMemoryServerKeyManager(),
		sessions:   session.NewMemoryManager(),
		transport:  &transport.TCPTransport{},
		services:   make(map[string]*serviceInfo),
		shutdownCh: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(s)
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
	if impl == nil {
		panic("implementation cannot be nil")
	}
	if _, exists := s.services[desc.ServiceName]; exists {
		panic(fmt.Sprintf("service %s already registered", desc.ServiceName))
	}

	s.services[desc.ServiceName] = &serviceInfo{desc: desc, impl: impl}
}

// Serve starts serving on the given listener
func (s *Server) Serve(lis net.Listener) error {
	for {
		select {
		case <-s.shutdownCh:
			return nil
		default:
		}

		conn, err := lis.Accept()
		if err != nil {
			return err
		}
		h := &connHandler{
			server: s,
			conn:   newNetConn(conn),
		}
		go func() {
			_ = h.run()
		}()
	}
}

// ServeTransport starts serving on a transport.Listener.
func (s *Server) ServeTransport(lis transport.Listener) error {
	for {
		select {
		case <-s.shutdownCh:
			return nil
		default:
		}

		conn, err := lis.Accept()
		if err != nil {
			return err
		}
		h := &connHandler{
			server: s,
			conn:   conn,
		}
		go func() {
			_ = h.run()
		}()
	}
}

// Stop stops the server
func (s *Server) Stop() error {
	select {
	case <-s.shutdownCh:
	default:
		close(s.shutdownCh)
	}
	return nil
}

// ServiceDesc describes a service for registration.
// (ServiceDesc and MethodDesc are defined above)

// Session represents a user session.
type Session = session.Session

// SessionStore interface for session storage.
type SessionStore interface {
	Get(authKeyID int64) (*legacySession, error)
	Save(session *legacySession) error
	Delete(authKeyID int64) error
}

// TLObject interface for Telegram objects
type TLObject interface {
	ConstructorID() uint32
}

// Transport interface for network transports.
type Transport = transport.Transport

// Listener interface for accepting connections.
type Listener = transport.Listener

// Conn interface for connections.
type Conn = transport.Conn

// Logger interface for logging.
type Logger interface {
	Info(msg string, args ...interface{})
	Error(msg string, args ...interface{})
	Debug(msg string, args ...interface{})
}

// dispatcher handles internal registration and dispatch of TL objects and methods
type dispatcher struct {
	mu           sync.RWMutex
	constructors map[uint32]func() TLObject
	methods      map[uint32]func(context.Context, TLObject) (interface{}, error)
}

// newDispatcher creates a new dispatcher
func newDispatcher() *dispatcher {
	return &dispatcher{
		constructors: make(map[uint32]func() TLObject),
		methods:      make(map[uint32]func(context.Context, TLObject) (interface{}, error)),
	}
}

// RegisterConstructor registers a constructor function for a TL object type
func (d *dispatcher) RegisterConstructor(id uint32, constructor func() TLObject) {
	d.mu.Lock()
	d.constructors[id] = constructor
	d.mu.Unlock()
}

// RegisterMethod registers a method handler for RPC calls
func (d *dispatcher) RegisterMethod(id uint32, handler func(context.Context, TLObject) (interface{}, error)) {
	d.mu.Lock()
	d.methods[id] = handler
	d.mu.Unlock()
}

// LookupConstructor returns a constructor function for the given ID
func (d *dispatcher) LookupConstructor(id uint32) (func() TLObject, bool) {
	d.mu.RLock()
	constructor, ok := d.constructors[id]
	d.mu.RUnlock()
	return constructor, ok
}

// LookupMethod returns a method handler for the given ID
func (d *dispatcher) LookupMethod(id uint32) (func(context.Context, TLObject) (interface{}, error), bool) {
	d.mu.RLock()
	handler, ok := d.methods[id]
	d.mu.RUnlock()
	return handler, ok
}

// RegisterConstructor registers a constructor function globally
func RegisterConstructor(id uint32, constructor func() TLObject) {
	globalDispatcher.RegisterConstructor(id, constructor)
}

// RegisterMethod registers a method handler globally
func RegisterMethod(id uint32, handler func(context.Context, TLObject) (interface{}, error)) {
	globalDispatcher.RegisterMethod(id, handler)
}

// global dispatcher instance
var globalDispatcher = newDispatcher()

// ServiceDesc describes a service for registration.
type ServiceDesc struct {
	ServiceName string
	HandlerType interface{}
	Methods     []MethodDesc
}

// MethodDesc describes a method within a service.
type MethodDesc struct {
	MethodName string
	Handler    interface{} // Handler function (various signatures supported)
}

// HandshakeHandler handles unencrypted handshake messages
type HandshakeHandler interface {
	HandleUnencrypted(ctx context.Context, msgID int64, data []byte) ([]byte, error)
}

// UnaryServerInfo provides information about the current RPC call.
type UnaryServerInfo struct {
	// FullMethod is the full RPC method string, i.e., /package.service/method.
	FullMethod string
}

// UnaryHandler defines the handler invoked by UnaryServerInterceptor to complete the normal
// execution of a unary RPC.
type UnaryHandler func(ctx context.Context, req interface{}) (interface{}, error)

// UnaryInterceptor provides a hook to intercept the execution of a unary RPC on the server.
// The first UnaryInterceptor is called with the context, request, and UnaryServerInfo for the RPC.
// The interceptor can mutate the context and request, but must call handler to complete the RPC.
type UnaryInterceptor func(ctx context.Context, req interface{}, info *UnaryServerInfo, handler UnaryHandler) (resp interface{}, err error)

// Authorizer validates a request and returns an error if unauthorized.
type Authorizer interface {
	Authorize(ctx context.Context, req interface{}) error
}

// Handler represents a request handler function.
type Handler func(ctx context.Context, req interface{}) (interface{}, error)

// Interceptor represents middleware for request/response processing.
type Interceptor func(next Handler) Handler

// ServerOption represents server configuration options
type ServerOption func(*Server)

// WithTransport sets the transport for the server
func WithTransport(t Transport) ServerOption {
	return func(s *Server) {
		s.transport = t
	}
}

// WithMaxLayer sets the maximum supported layer version
func WithMaxLayer(layer int) ServerOption {
	return func(s *Server) {
		s.maxLayer = layer
	}
}

// WithLayers sets the supported layer versions
func WithLayers(layers ...int) ServerOption {
	return func(s *Server) {
		s.layers = append([]int(nil), layers...)
	}
}

// WithUnaryInterceptor adds a unary interceptor to the server (gRPC-like).
func WithUnaryInterceptor(i UnaryInterceptor) ServerOption {
	return func(s *Server) {
		s.unaryInterceptors = append(s.unaryInterceptors, i)
	}
}

// WithInterceptor adds a legacy interceptor to the server (deprecated, use WithUnaryInterceptor).
func WithInterceptor(i Interceptor) ServerOption {
	return func(s *Server) {
		s.legacyInterceptors = append(s.legacyInterceptors, i)
	}
}

// WithSessionStore sets the session store for the server
func WithSessionStore(store SessionStore) ServerOption {
	return func(s *Server) {
		s.sessions = newSessionAdapter(store)
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

// WithSessionManager sets the session manager.
func WithSessionManager(manager session.Manager) ServerOption {
	return func(s *Server) {
		if manager != nil {
			s.sessions = manager
		}
	}
}

// WithMaxMessageSize sets the maximum message size in bytes.
func WithMaxMessageSize(size int) ServerOption {
	return func(s *Server) {
		// TODO: implement max message size
	}
}

// WithMaxConcurrentStreams sets the maximum number of concurrent streams.
func WithMaxConcurrentStreams(n int) ServerOption {
	return func(s *Server) {
		// TODO: implement concurrent stream limiting
	}
}

// WithReadTimeout sets the read timeout for connections.
func WithReadTimeout(timeout time.Duration) ServerOption {
	return func(s *Server) {
		// TODO: implement read timeout
	}
}

// WithWriteTimeout sets the write timeout for connections.
func WithWriteTimeout(timeout time.Duration) ServerOption {
	return func(s *Server) {
		// TODO: implement write timeout
	}
}

type legacySession struct {
	ID        int64
	AuthKeyID int64
	Layer     int
	UserID    int64
	Data      map[string]interface{}
}

type sessionAdapter struct {
	store SessionStore
}

func newSessionAdapter(store SessionStore) session.Manager {
	if store == nil {
		return session.NewMemoryManager()
	}
	return &sessionAdapter{store: store}
}

func (s *sessionAdapter) Get(authKeyID crypto.KeyID) (*session.Session, error) {
	legacy, err := s.store.Get(int64(authKeyID))
	if err != nil {
		return nil, err
	}
	return &session.Session{
		ID:        legacy.ID,
		AuthKeyID: authKeyID,
		Layer:     legacy.Layer,
		UserID:    legacy.UserID,
		Data:      syncMapFromLegacy(legacy.Data),
	}, nil
}

func (s *sessionAdapter) Create(authKeyID crypto.KeyID) (*session.Session, error) {
	sess := &session.Session{
		ID:        time.Now().UnixNano(),
		AuthKeyID: authKeyID,
		Layer:     0,
		UserID:    0,
	}
	legacy := legacySessionFrom(sess)
	if err := s.store.Save(legacy); err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *sessionAdapter) Save(sess *session.Session) error {
	return s.store.Save(legacySessionFrom(sess))
}

func (s *sessionAdapter) Delete(authKeyID crypto.KeyID) error {
	return s.store.Delete(int64(authKeyID))
}

func (s *sessionAdapter) GC(maxAge time.Duration) {
}

func legacySessionFrom(sess *session.Session) *legacySession {
	legacy := &legacySession{
		ID:        sess.ID,
		AuthKeyID: int64(sess.AuthKeyID),
		Layer:     sess.Layer,
		UserID:    sess.UserID,
		Data:      map[string]interface{}{},
	}
	sess.Data.Range(func(key, value interface{}) bool {
		if k, ok := key.(string); ok {
			legacy.Data[k] = value
		}
		return true
	})
	return legacy
}

func syncMapFromLegacy(data map[string]interface{}) sync.Map {
	var m sync.Map
	for k, v := range data {
		m.Store(k, v)
	}
	return m
}

// ChainUnaryInterceptors chains multiple unary interceptors together.
func ChainUnaryInterceptors(interceptors ...UnaryInterceptor) UnaryInterceptor {
	return func(ctx context.Context, req interface{}, info *UnaryServerInfo, handler UnaryHandler) (interface{}, error) {
		// Apply interceptors in reverse order so first interceptor is outermost
		chainedHandler := handler
		for i := len(interceptors) - 1; i >= 0; i-- {
			currentInterceptor := interceptors[i]
			nextHandler := chainedHandler
			chainedHandler = func(ctx context.Context, req interface{}) (interface{}, error) {
				return currentInterceptor(ctx, req, info, nextHandler)
			}
		}
		return chainedHandler(ctx, req)
	}
}

// ChainInterceptors chains multiple legacy interceptors together.
func ChainInterceptors(interceptors ...Interceptor) Interceptor {
	return func(next Handler) Handler {
		for i := len(interceptors) - 1; i >= 0; i-- {
			next = interceptors[i](next)
		}
		return next
	}
}

// RecoveryInterceptor recovers panics and uses errorFactory to build errors.
func RecoveryInterceptor(errorFactory func(message string) error) UnaryInterceptor {
	return func(ctx context.Context, req interface{}, info *UnaryServerInfo, handler UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				if errorFactory != nil {
					err = errorFactory(fmt.Sprintf("panic: %v", r))
					return
				}
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		return handler(ctx, req)
	}
}

// LoggingInterceptor logs request/response lifecycle.
func LoggingInterceptor(logger Logger) UnaryInterceptor {
	return func(ctx context.Context, req interface{}, info *UnaryServerInfo, handler UnaryHandler) (interface{}, error) {
		if logger != nil {
			logger.Info("tlrpc request", "method", info.FullMethod, "type", fmt.Sprintf("%T", req))
		}
		resp, err := handler(ctx, req)
		if logger != nil {
			if err != nil {
				logger.Error("tlrpc response", "method", info.FullMethod, "error", err)
			} else {
				logger.Info("tlrpc response", "method", info.FullMethod, "type", fmt.Sprintf("%T", resp))
			}
		}
		return resp, err
	}
}

// AuthInterceptor checks authorization.
func AuthInterceptor(authorizer Authorizer) UnaryInterceptor {
	return func(ctx context.Context, req interface{}, info *UnaryServerInfo, handler UnaryHandler) (interface{}, error) {
		if authorizer == nil {
			return handler(ctx, req)
		}
		if err := authorizer.Authorize(ctx, req); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}
