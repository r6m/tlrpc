# Lifecycle Hooks

`tlrpc` exposes optional session lifecycle hooks so applications can implement presence, routing, or fanout with external systems (Redis, NATS) without adding those dependencies to core.

## Hook Registration

```go
srv := tlrpc.NewServer(
	tlrpc.WithOnSessionBound(func(binding tlrpc.Binding, conn tlrpc.Conn) {
		// Notify external routing or presence systems.
	}),
	tlrpc.WithOnSessionUnbound(func(binding tlrpc.Binding) {
		// Cleanup routing or presence state.
	}),
)
```

Hooks are optional. Keep hook handlers fast and non-blocking.

## Binding Metadata

`Binding` includes:

- `AuthKeyID`
- `SessionID`
- `ServerSalt`
- `UserID` (0 if unauthenticated)
- `Layer`
- `DCID` (if set)

You can also derive bindings from request context:

```go
binding, ok := tlrpc.BindingFromContext(ctx)
```

## External Routing Examples (Pseudocode)

### Redis Presence

```
OnSessionBound(binding, conn):
  redis.SET("presence:user:"+binding.UserID, "online")

OnSessionUnbound(binding):
  redis.DEL("presence:user:"+binding.UserID)
```

### NATS Routing

```
OnSessionBound(binding, conn):
  nats.Publish("routing.user", binding)

OnSessionUnbound(binding):
  nats.Publish("routing.user", binding)
```

These examples are intentionally schematic. Application code controls data formats, retries, and consistency.

## Local Push (No Routing)

Handlers can send a server-initiated TL object on the active connection:

```go
func (s *MyService) Notify(ctx context.Context, _ *gen.SomeRequest) (gen.UpdatesType, error) {
	if conn, ok := tlrpc.ConnFromContext(ctx); ok {
		_ = conn.Send(&gen.UpdateUserStatus{UserID: 1, Status: &gen.UserStatusOnline{Expires: 123}})
	}
	return &gen.UpdatesTooLong{}, nil
}
```

`Conn.Send` only writes to the current connection. `tlrpc` does not manage which users should receive updates or any fanout. External routing/presence systems remain application concerns.
