package tlrpc

import (
	"context"

	"github.com/r6m/tlrpc/registry"
)

type contextKey string

const (
	contextKeySession contextKey = "tlrpc.session"
	contextKeyLayer   contextKey = "tlrpc.layer"
	contextKeyAuthKey contextKey = "tlrpc.auth_key_id"
	contextKeyUserID  contextKey = "tlrpc.user_id"
)

// Authorizer validates a request and returns an error if unauthorized.
type Authorizer = registry.Authorizer

// RecoveryInterceptor recovers panics and returns an RPCError.
func RecoveryInterceptor() Interceptor {
	return registry.RecoveryInterceptor(func(message string) error {
		return &RPCError{Code: 500, Message: message}
	})
}

// LoggingInterceptor logs request/response lifecycle.
func LoggingInterceptor(logger Logger) Interceptor {
	return registry.LoggingInterceptor(logger)
}

// AuthInterceptor checks authorization.
func AuthInterceptor(authorizer Authorizer) Interceptor {
	return registry.AuthInterceptor(authorizer)
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
