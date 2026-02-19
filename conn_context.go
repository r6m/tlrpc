package tlrpc

import (
	"context"
)

type contextKeyConn struct{}

// ConnFromContext returns the active connection from context, if available.
func ConnFromContext(ctx context.Context) (Conn, bool) {
	if ctx == nil {
		return nil, false
	}
	if value := ctx.Value(contextKeyConn{}); value != nil {
		if conn, ok := value.(Conn); ok {
			return conn, true
		}
	}
	return nil, false
}

func withConn(ctx context.Context, conn Conn) context.Context {
	return context.WithValue(ctx, contextKeyConn{}, conn)
}
