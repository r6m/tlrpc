package tlrpc

import (
	"context"
	"fmt"

	"github.com/r6m/tlrpc/session"
)

type contextKey string

const (
	contextKeyLayer   contextKey = "tlrpc.layer"
	contextKeyAuthKey contextKey = "tlrpc.auth_key_id"
	contextKeyUserID  contextKey = "tlrpc.user_id"
	contextKeyClient  contextKey = "tlrpc.client_metadata"
	contextKeySender  contextKey = "tlrpc.sender"
	contextKeyBinding contextKey = "tlrpc.binding"
)

// IncomingMD returns the incoming metadata in ctx if it exists.
func IncomingMD(ctx context.Context) (MD, bool) {
	return FromIncomingContext(ctx)
}

// OutgoingMD returns the outgoing metadata in ctx if it exists.
func OutgoingMD(ctx context.Context) (MD, bool) {
	return FromOutgoingContext(ctx)
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

// ClientMetadataFromContext returns immutable initConnection metadata.
func ClientMetadataFromContext(ctx context.Context) (session.ClientMetadata, bool) {
	if ctx == nil {
		return session.ClientMetadata{}, false
	}
	metadata, ok := ctx.Value(contextKeyClient).(session.ClientMetadata)
	return metadata, ok
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

func withClientMetadata(ctx context.Context, metadata session.ClientMetadata) context.Context {
	return context.WithValue(ctx, contextKeyClient, metadata)
}

func withBinding(ctx context.Context, binding Binding) context.Context {
	return context.WithValue(ctx, contextKeyBinding, binding)
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

// RecoveryInterceptor recovers interceptor panics as a generic internal error.
// Generated application handlers are protected independently at the mandatory
// runtime application boundary, so installing this interceptor is optional.
func RecoveryInterceptor() UnaryInterceptor {
	return func(ctx context.Context, req interface{}, info *UnaryServerInfo, handler UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if recover() != nil {
				resp = nil
				err = genericInternalError()
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
