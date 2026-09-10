// Package tlrpc provides a schema-first TL RPC framework for Go servers and
// clients. Applications supply their own TL schema, generate service contracts,
// and implement those contracts; Telegram is one supported MTProto consumer.
package tlrpc

import (
	"context"
	"fmt"

	"github.com/r6m/tlrpc/transport"
)

// TLObject is the common constructor identity implemented by generated TL values.
type TLObject interface {
	ConstructorID() uint32
}

// Transport interface for network transports.
type Transport = transport.Transport

// Listener interface for accepting connections.
type Listener = transport.Listener

// Logger interface for logging.
type Logger interface {
	Info(msg string, args ...interface{})
	Error(msg string, args ...interface{})
	Debug(msg string, args ...interface{})
}

// ServiceDesc describes a service for registration.
type ServiceDesc struct {
	ServiceName string
	// SchemaLayer is the layer represented by the generated package. Zero means
	// the application supplied an unlayered schema.
	SchemaLayer int
	HandlerType interface{}
	Methods     []MethodDesc
}

// MethodHandler is the erased dispatch ABI used by generated service adapters.
// Generated adapters check the concrete request type before directly invoking
// the typed service method.
type MethodHandler func(srv any, ctx context.Context, req TLObject) (any, error)

// BindMethod adapts a handwritten typed service adapter to MethodHandler
// without reflection.
func BindMethod[Req TLObject, Resp any](handler func(any, context.Context, Req) (Resp, error)) MethodHandler {
	return func(srv any, ctx context.Context, req TLObject) (any, error) {
		typed, ok := req.(Req)
		if !ok {
			return nil, fmt.Errorf("tlrpc: request %T does not satisfy the declared method input", req)
		}
		return handler(srv, ctx, typed)
	}
}

// MethodDesc describes a method within a service.
type MethodDesc struct {
	// MinLayer and MaxLayer bound this wire variant inclusively. Zero leaves
	// that side unbounded. Repeated rows may describe disjoint intervals for
	// the same generated method contract.
	MinLayer      int
	MaxLayer      int
	MethodName    string
	ConstructorID uint32          // TL constructor ID for the request method.
	NewRequest    func() TLObject // Constructs an empty request object for decoding.
	Handler       MethodHandler   // Erased adapter for the generated typed service method.
	// EncodeResponse is generated for the method's exact result type. It checks
	// interceptor output and encodes it using the effective layer and limits.
	EncodeResponse func(any, int, EncodeLimits) ([]byte, error)
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
