package tlrpc

import (
	"context"
	"fmt"
)

type contextKey string

const (
	contextKeySession contextKey = "tlrpc.session"
	contextKeyLayer   contextKey = "tlrpc.layer"
	contextKeyAuthKey contextKey = "tlrpc.auth_key_id"
	contextKeyUserID  contextKey = "tlrpc.user_id"
)

// IncomingMD returns the incoming metadata in ctx if it exists.
func IncomingMD(ctx context.Context) (MD, bool) {
	return FromIncomingContext(ctx)
}

// OutgoingMD returns the outgoing metadata in ctx if it exists.
func OutgoingMD(ctx context.Context) (MD, bool) {
	return FromOutgoingContext(ctx)
}


// SessionFromContext returns the session from context.
func SessionFromContext(ctx context.Context) *Session {
	if ctx == nil {
		return nil
	}
	if value := ctx.Value(contextKeySession); value != nil {
		if session, ok := value.(*Session); ok {
			return session
		}
	}
	return nil
}

// LayerFromContext returns the layer from context.
func LayerFromContext(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	if value := ctx.Value(contextKeyLayer); value != nil {
		if layer, ok := value.(int); ok {
			return layer
		}
	}
	return 0
}

// AuthKeyIDFromContext returns the auth key ID from context.
func AuthKeyIDFromContext(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	if value := ctx.Value(contextKeyAuthKey); value != nil {
		if id, ok := value.(int64); ok {
			return id
		}
	}
	return 0
}

// UserIDFromContext returns the user ID from context.
func UserIDFromContext(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	if value := ctx.Value(contextKeyUserID); value != nil {
		if id, ok := value.(int64); ok {
			return id
		}
	}
	return 0
}

func withSession(ctx context.Context, session *Session) context.Context {
	return context.WithValue(ctx, contextKeySession, session)
}

func withLayer(ctx context.Context, layer int) context.Context {
	return context.WithValue(ctx, contextKeyLayer, layer)
}

func withAuthKeyID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, contextKeyAuthKey, id)
}

func withUserID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, contextKeyUserID, id)
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
