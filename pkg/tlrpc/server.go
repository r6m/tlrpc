// Package tlrpc provides the core Telegram RPC server framework.
package tlrpc

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/r6m/tlrpc/pkg/layer"
	"github.com/r6m/tlrpc/pkg/session"
	"github.com/r6m/tlrpc/pkg/transport"
)

// Server is a Telegram RPC server.
type Server struct {
	transport    transport.Transport
	registry     *serviceRegistry
	sessions     session.Manager
	layers       *layer.Registry
	interceptors []Interceptor
	maxLayer     int

	mu        sync.RWMutex
	listeners []net.Listener
	conns     map[net.Conn]struct{}
	closed    bool
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// NewServer creates a new TLRPC server.
func NewServer(opts ...ServerOption) *Server {
	s := &Server{
		registry: newServiceRegistry(),
		sessions: session.NewMemoryManager(),
		layers:   layer.NewRegistry(),
		conns:    make(map[net.Conn]struct{}),
		maxLayer: 222,
	}

	for _, opt := range opts {
		opt(s)
	}

	if s.transport == nil {
		s.transport = transport.NewTCP()
	}

	return s
}

// WithTransport sets the transport.
func WithTransport(t transport.Transport) ServerOption {
	return func(s *Server) {
		s.transport = t
	}
}

// WithMaxLayer sets the maximum supported layer.
func WithMaxLayer(layer int) ServerOption {
	return func(s *Server) {
		s.maxLayer = layer
	}
}

// WithLayers registers supported layers.
func WithLayers(layers ...int) ServerOption {
	return func(s *Server) {
		for _, l := range layers {
			s.layers.Register(l)
		}
	}
}

// WithInterceptor adds an interceptor.
func WithInterceptor(i Interceptor) ServerOption {
	return func(s *Server) {
		s.interceptors = append(s.interceptors, i)
	}
}

// WithSessionStore sets the session store.
func WithSessionStore(store session.Store) ServerOption {
	return func(s *Server) {
		s.sessions = session.NewManager(store)
	}
}

// RegisterService registers a service implementation.
func (s *Server) RegisterService(desc ServiceDesc, impl interface{}) {
	s.registry.register(desc, impl)
}

// Serve accepts connections on the listener.
func (s *Server) Serve(lis net.Listener) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("server closed")
	}
	s.listeners = append(s.listeners, lis)
	s.mu.Unlock()

	for {
		conn, err := lis.Accept()
		if err != nil {
			if s.isClosed() {
				return nil
			}
			return err
		}

		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()

		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		conn.Close()
	}()

	// Create connection handler
	handler := newConnHandler(s, conn)
	handler.run()
}

// Stop gracefully stops the server.
func (s *Server) Stop() error {
	s.mu.Lock()
	s.closed = true
	listeners := s.listeners
	conns := s.conns
	s.mu.Unlock()

	for _, lis := range listeners {
		lis.Close()
	}

	for conn := range conns {
		conn.Close()
	}

	return nil
}

func (s *Server) isClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

// ServiceDesc describes a service.
type ServiceDesc struct {
	ServiceName string
	Methods     []MethodDesc
}

// MethodDesc describes a method.
type MethodDesc struct {
	MethodName string
	Handler    Handler
}

// Handler handles RPC calls.
type Handler func(ctx context.Context, req interface{}) (interface{}, error)

// Interceptor intercepts RPC calls.
type Interceptor func(next Handler) Handler
