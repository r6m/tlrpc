package tlrpc

import "context"

// Binding represents the session/connection binding metadata.
type Binding struct {
	AuthKeyID  int64
	SessionID  int64
	ServerSalt int64
	UserID     int64
	Layer      int
}

// BindingFromContext derives a binding from known context values.
func BindingFromContext(ctx context.Context) (Binding, bool) {
	if ctx == nil {
		return Binding{}, false
	}
	binding, ok := ctx.Value(contextKeyBinding).(Binding)
	if !ok {
		return Binding{}, false
	}
	if binding.AuthKeyID == 0 && binding.SessionID == 0 && binding.UserID == 0 && binding.Layer == 0 {
		return Binding{}, false
	}
	return binding, true
}
