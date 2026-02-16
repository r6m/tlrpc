// Package tlrpc provides the core framework for Telegram RPC servers and clients.
package tlrpc

import (
	"net"
	"sync"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/registry"
	"github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
)

// Legacy error variables - moved to error.go

// Server represents an RPC server
type Server struct {
	transport        Transport
	authKeys         crypto.AuthKeyManager
	sessions         session.Manager
	codec            Codec
	maxLayer         int
	layers           []int
	unaryInterceptors []UnaryInterceptor  // New gRPC-like interceptors
	legacyInterceptors []Interceptor      // Legacy support
	logger           Logger
	registry         *registry.Registry
	shutdownCh       chan struct{}
}

// NewServer creates a new RPC server with the given options
func NewServer(opts ...ServerOption) *Server {
	s := &Server{
		registry:   registry.New(),
		authKeys:   crypto.NewMemoryAuthKeyManager(),
		sessions:   session.NewMemoryManager(),
		transport:  &transport.TCPTransport{},
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
	if s.registry == nil {
		s.registry = registry.New()
	}
	if err := s.registry.Register(desc, impl); err != nil {
		panic(err)
	}
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
type ServiceDesc = registry.ServiceDesc

// MethodDesc describes a method within a service.
type MethodDesc = registry.MethodDesc

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
	Method() string // for RPC types
}

// Codec handles TL object encoding/decoding.
type Codec interface {
	Decode(layer int, data []byte) (TLObject, error)
	Encode(layer int, obj TLObject) ([]byte, error)
}

// Transport interface for network transports.
type Transport = transport.Transport

// Listener interface for accepting connections.
type Listener = transport.Listener

// Conn interface for connections.
type Conn = transport.Conn

// Logger interface for logging.
type Logger = registry.Logger

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

// WithSessionManager sets the session manager.
func WithSessionManager(manager session.Manager) ServerOption {
	return func(s *Server) {
		if manager != nil {
			s.sessions = manager
		}
	}
}

// WithCodec sets the codec for TL object encoding/decoding.
func WithCodec(codec Codec) ServerOption {
	return func(s *Server) {
		s.codec = codec
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
