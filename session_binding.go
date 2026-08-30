package tlrpc

import (
	"context"
	"errors"

	runtimev2 "github.com/r6m/tlrpc/internal/runtime"
)

var (
	ErrSessionUnavailable = errors.New("tlrpc: protocol session unavailable")
	ErrInvalidUserID      = errors.New("tlrpc: user ID must be positive")
)

// BindSessionUser marks the active protocol session as authenticated for an
// application-owned user. TLRPC persists the changed session after the handler
// returns and exposes the user ID to subsequent handlers and delivery hooks.
//
// The user ID is opaque to the framework; its meaning and authentication are
// entirely owned by the consuming application.
func BindSessionUser(ctx context.Context, userID int64) error {
	if userID <= 0 {
		return ErrInvalidUserID
	}
	if ctx == nil {
		return ErrSessionUnavailable
	}
	collector, ok := ctx.Value(contextKeyRuntimeMutations{}).(*runtimeMutationCollector)
	if !ok || collector == nil {
		return ErrSessionUnavailable
	}
	collector.append(runtimev2.BindUser{UserID: userID})
	return nil
}

// UnbindSessionUser clears an application user binding from the active
// protocol session. The change is persisted after the handler returns.
func UnbindSessionUser(ctx context.Context) error {
	if ctx == nil {
		return ErrSessionUnavailable
	}
	collector, ok := ctx.Value(contextKeyRuntimeMutations{}).(*runtimeMutationCollector)
	if !ok || collector == nil {
		return ErrSessionUnavailable
	}
	collector.append(runtimev2.UnbindUser{})
	return nil
}
