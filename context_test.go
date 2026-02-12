package tlrpc

import (
	"context"
	"testing"
)

func TestContextHelpers(t *testing.T) {
	session := &Session{ID: 1, AuthKeyID: 2, Layer: 3, UserID: 4}
	ctx := context.Background()
	ctx = withSession(ctx, session)
	ctx = withLayer(ctx, 3)
	ctx = withAuthKeyID(ctx, 2)
	ctx = withUserID(ctx, 4)

	if got := SessionFromContext(ctx); got != session {
		t.Fatalf("expected session")
	}
	if LayerFromContext(ctx) != 3 {
		t.Fatalf("expected layer")
	}
	if AuthKeyIDFromContext(ctx) != 2 {
		t.Fatalf("expected auth key")
	}
	if UserIDFromContext(ctx) != 4 {
		t.Fatalf("expected user id")
	}
}
