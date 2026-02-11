package tlrpc

import (
	"context"
)

// contextKey is used for context values.
type contextKey string

const (
	sessionKey   contextKey = "tlrpc.session"
	layerKey     contextKey = "tlrpc.layer"
	authKeyIDKey contextKey = "tlrpc.auth_key_id"
	userIDKey    contextKey = "tlrpc.user_id"
)

// WithSession adds session to context.
func WithSession(ctx context.Context, session interface{}) context.Context {
	return context.WithValue(ctx, sessionKey, session)
}

// SessionFromContext extracts session from context.
func SessionFromContext(ctx context.Context) interface{} {
	return ctx.Value(sessionKey)
}

// WithLayer adds layer to context.
func WithLayer(ctx context.Context, layer int) context.Context {
	return context.WithValue(ctx, layerKey, layer)
}

// LayerFromContext extracts layer from context.
func LayerFromContext(ctx context.Context) int {
	if val := ctx.Value(layerKey); val != nil {
		if layer, ok := val.(int); ok {
			return layer
		}
	}
	return 0
}

// WithAuthKeyID adds auth key ID to context.
func WithAuthKeyID(ctx context.Context, authKeyID int64) context.Context {
	return context.WithValue(ctx, authKeyIDKey, authKeyID)
}

// AuthKeyIDFromContext extracts auth key ID from context.
func AuthKeyIDFromContext(ctx context.Context) int64 {
	if val := ctx.Value(authKeyIDKey); val != nil {
		if id, ok := val.(int64); ok {
			return id
		}
	}
	return 0
}

// WithUserID adds user ID to context.
func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// UserIDFromContext extracts user ID from context.
func UserIDFromContext(ctx context.Context) int64 {
	if val := ctx.Value(userIDKey); val != nil {
		if id, ok := val.(int64); ok {
			return id
		}
	}
	return 0
}