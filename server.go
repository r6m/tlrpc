// Package tlrpc provides the core framework for Telegram RPC servers and clients.
package tlrpc

import (
	"fmt"
	"net"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
)

// Server represents an RPC server
type Server struct {
	transport         Transport
	authKeys          crypto.AuthKeyManager
	serverKeys        crypto.ServerKeyManager
	sessions          session.Manager
	dispatcher        *dispatcher
	maxLayer          int
	layers            []int
	unaryInterceptors []UnaryInterceptor // New gRPC-like interceptors
	logger            Logger
	services          map[string]*serviceInfo // for backward compatibility
	handshakeHandler  HandshakeHandler
	shutdownCh        chan struct{}
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

// WithSessionStore sets the session store for the server
func WithSessionStore(store session.SessionStore) ServerOption {
	return func(s *Server) {
		s.sessions = session.NewSessionAdapter(store)
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
