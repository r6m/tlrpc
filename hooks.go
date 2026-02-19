package tlrpc

// OnSessionBoundHook is invoked when a connection is bound to a session/user.
type OnSessionBoundHook func(Binding, Conn)

// OnSessionUnboundHook is invoked when a connection is closed/unbound.
type OnSessionUnboundHook func(Binding)
