// Package tlrpc provides the core framework for Telegram RPC servers and clients.
package tlrpc

import (
	"context"

	"github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
)

// Session represents a user session.
type Session = session.Session

// SessionStore interface for session storage (legacy, for backward compatibility).
type SessionStore = session.SessionStore

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

// ServiceDesc describes a service for registration.
type ServiceDesc struct {
	ServiceName string
	HandlerType interface{}
	Methods     []MethodDesc
}

// MethodDesc describes a method within a service.
type MethodDesc struct {
	MethodName    string
	ConstructorID uint32          // TL constructor ID for the request method.
	NewRequest    func() TLObject // Constructs an empty request object for decoding.
	Handler       interface{}     // Handler function (various signatures supported)
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
