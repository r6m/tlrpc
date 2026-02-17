// Package tlrpc provides the core framework for Telegram RPC servers and clients.
package tlrpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"time"

	"github.com/r6m/tlrpc/crypto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
	"github.com/r6m/tlrpc/types"
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
	s.registerDefaultConstructors()
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Server) registerDefaultConstructors() {
	// Built-in TL primitives.
	for id, ctor := range types.GetBaseConstructors() {
		c := ctor
		s.RegisterConstructor(id, func() TLObject { return c() })
	}

	// MTProto envelope/control objects.
	s.RegisterConstructor(mtprototl.MsgContainerID, func() TLObject { return &mtprototl.MsgContainer{} })
	s.RegisterConstructor(mtprototl.MsgsAckID, func() TLObject { return &mtprototl.MsgsAck{} })
	s.RegisterConstructor(mtprototl.GzipPackedID, func() TLObject { return &mtprototl.GzipPacked{} })
	s.RegisterConstructor(mtprototl.RPCResultID, func() TLObject { return &mtprototl.RPCResult{} })
	s.RegisterConstructor(mtprototl.RPCErrorID, func() TLObject { return &mtprototl.RPCError{} })
	s.RegisterConstructor(mtprototl.MsgResendReqID, func() TLObject { return &mtprototl.MsgResendReq{} })
	s.RegisterConstructor(mtprototl.MsgsStateReqID, func() TLObject { return &mtprototl.MsgsStateReq{} })
	s.RegisterConstructor(mtprototl.MsgsStateInfoID, func() TLObject { return &mtprototl.MsgsStateInfo{} })
}

// RegisterConstructor registers a TL constructor in the server dispatcher.
func (s *Server) RegisterConstructor(id uint32, ctor func() TLObject) {
	s.dispatcher.RegisterConstructor(id, ctor)
}

// RegisterMethod registers a method handler in the server dispatcher.
func (s *Server) RegisterMethod(id uint32, h func(context.Context, TLObject) (interface{}, error)) {
	s.dispatcher.RegisterMethod(id, h)
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

	if desc.HandlerType != nil {
		handlerType := reflect.TypeOf(desc.HandlerType)
		implType := reflect.TypeOf(impl)
		if handlerType.Kind() == reflect.Ptr && handlerType.Elem().Kind() == reflect.Interface {
			if !implType.Implements(handlerType.Elem()) {
				panic(fmt.Sprintf("implementation %T does not satisfy handler type %v", impl, handlerType.Elem()))
			}
		}
	}

	for _, method := range desc.Methods {
		methodID := method.ConstructorID
		newReq := method.NewRequest

		// Backward-compatible inference for descriptors that don't populate NewRequest/ConstructorID.
		if inferredID, inferredReq, ok := inferMethodRequestFromHandler(method.Handler); ok {
			if methodID == 0 {
				methodID = inferredID
			}
			if newReq == nil {
				newReq = inferredReq
			}
		}

		if methodID == 0 {
			panic(fmt.Sprintf("method %q is missing constructor ID", method.MethodName))
		}
		if newReq == nil {
			panic(fmt.Sprintf("method %q is missing request constructor", method.MethodName))
		}
		if _, exists := s.dispatcher.LookupMethod(methodID); exists {
			panic(fmt.Sprintf("duplicate method constructor ID 0x%08x for %q", methodID, method.MethodName))
		}
		if _, exists := s.dispatcher.LookupConstructor(methodID); exists {
			panic(fmt.Sprintf("duplicate request constructor ID 0x%08x for %q", methodID, method.MethodName))
		}

		invoker, err := bindServiceMethodHandler(impl, method.Handler)
		if err != nil {
			panic(fmt.Sprintf("method %q handler bind failed: %v", method.MethodName, err))
		}

		s.dispatcher.RegisterConstructor(methodID, newReq)
		s.dispatcher.RegisterMethod(methodID, invoker)
	}

	s.services[desc.ServiceName] = &serviceInfo{desc: desc, impl: impl}
}

func inferMethodRequestFromHandler(handler interface{}) (uint32, func() TLObject, bool) {
	handlerValue := reflect.ValueOf(handler)
	if !handlerValue.IsValid() || handlerValue.Kind() != reflect.Func {
		return 0, nil, false
	}

	handlerType := handlerValue.Type()
	if handlerType.NumIn() < 3 {
		return 0, nil, false
	}
	reqType := handlerType.In(2)
	if reqType.Kind() != reflect.Ptr {
		return 0, nil, false
	}

	reqValue := reflect.New(reqType.Elem())
	reqObj, ok := reqValue.Interface().(TLObject)
	if !ok {
		return 0, nil, false
	}

	return reqObj.ConstructorID(), func() TLObject {
		return reflect.New(reqType.Elem()).Interface().(TLObject)
	}, true
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

// WithInterceptor adapts a legacy interceptor to a unary interceptor chain.
func WithInterceptor(i Interceptor) ServerOption {
	return func(s *Server) {
		if i == nil {
			return
		}
		s.unaryInterceptors = append(s.unaryInterceptors, func(ctx context.Context, req interface{}, info *UnaryServerInfo, handler UnaryHandler) (interface{}, error) {
			wrapped := i(func(ctx context.Context, req interface{}) (interface{}, error) {
				return handler(ctx, req)
			})
			return wrapped(ctx, req)
		})
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
