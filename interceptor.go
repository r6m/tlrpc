package tlrpc

import (
	"context"
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
