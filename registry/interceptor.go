package registry

import (
	"context"
	"fmt"
)

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

// Logger provides structured logging hooks.
type Logger interface {
	Info(msg string, args ...interface{})
	Error(msg string, args ...interface{})
	Debug(msg string, args ...interface{})
}

// Authorizer validates a request and returns an error if unauthorized.
type Authorizer interface {
	Authorize(ctx context.Context, req interface{}) error
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

// Legacy Interceptor support - moved to main package
