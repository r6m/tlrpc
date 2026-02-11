# TLRPC Design Philosophy

## Goals

1. **Simplicity**: Using TLRPC should feel like using gRPC or net/rpc
2. **Performance**: Match or exceed raw MTProto performance
3. **Correctness**: Strict MTProto compliance
4. **Flexibility**: Pluggable components for different use cases

## Non-Goals

1. **Complete Server**: We provide framework, not full Telegram server
2. **Client Library**: Focus is server-side (client for testing only)
3. **All Layers Forever**: Support recent layers (195+), drop ancient ones
4. **Magic**: No hidden upgrade/downgrade - explicit in user code

## Design Decisions

### Why No Automatic Layer Upgrade/Downgrade?

TLRPC generates per-layer types. If a client sends Layer 195 `sendMessage` and your service implements Layer 222, you handle the difference explicitly:

```go
func (s *Service) SendMessage(ctx context.Context, req interface{}) (interface{}, error) {
    switch r := req.(type) {
    case *layer195.SendMessageRequest:
        // Handle old format
    case *layer222.SendMessageRequest:
        // Handle new format
    }
}
```

This is explicit but correct. Magic conversions hide complexity and bugs.

### Why Generated Code?

- **Type Safety**: Catch errors at compile time
- **Performance**: No reflection in hot paths
- **Correctness**: Generated from official TL schema
- **Maintainability**: Update schema → regenerate → fix compile errors

### Why Interceptors Over Middleware?

Interceptors compose better:

```go
server := tlrpc.NewServer(
    tlrpc.WithInterceptor(authInterceptor),
    tlrpc.WithInterceptor(loggingInterceptor),
    tlrpc.WithInterceptor(metricsInterceptor),
)
```

Order matters and is explicit.

### Context is King

All handlers receive `context.Context`:

- Cancellation from client disconnect
- Deadlines for timeouts
- Values for session, layer, auth info

```go
func (s *Service) Method(ctx context.Context, req *Request) (*Response, error) {
    session := tlrpc.SessionFromContext(ctx)
    layer := tlrpc.LayerFromContext(ctx)
    // ...
}
```

## Code Generation Strategy

### Input: TL Schema

```
---types---
user#8f97c628 flags:# ... = User;
userEmpty#d3bc4b7c id:long = User;

---functions---
auth.sendCode#a677244f ... = auth.SentCode;
```

### Output: Go Code

```go
// Types
type User interface { isUser() }
type UserObj struct { /* fields */ }
func (UserObj) isUser() {}

// Service interface
type AuthServer interface {
    SendCode(context.Context, *SendCodeRequest) (*AuthSentCode, error)
}

// Registration
func RegisterAuthServer(*tlrpc.Server, AuthServer)
```

### Generation Rules

1. **PascalCase**: `send_code` → `SendCode`
2. **Pointers**: All structs are pointers
3. **Interfaces**: Polymorphic types (User, Peer) become interfaces
4. **Tags**: JSON tags for debugging, TL tags for serialization
5. **Comments**: Preserved from TL schema

## Testing Strategy

### Unit Tests

Test generated serialization:

```go
func TestSerialize(t *testing.T) {
    obj := &layer222.User{ID: 123, FirstName: "Test"}
    data, err := obj.SerializeTL()
    require.NoError(t, err)

    obj2 := &layer222.User{}
    err = obj2.DeserializeTL(data)
    require.NoError(t, err)

    assert.Equal(t, obj, obj2)
}
```

### Integration Tests

Test full server:

```go
func TestServer(t *testing.T) {
    srv := tlrpc.NewServer()
    RegisterTestService(srv, &testService{})

    client := tlrpc.Dial(srv.Listener())
    resp, err := client.Call(ctx, &Request{})
    assert.NoError(t, err)
}
```

## Security Considerations

1. **Auth Keys**: Never log, constant-time comparison
2. **Timing**: Constant-time crypto operations
3. **Validation**: Strict input validation on all handlers
4. **Limits**: Rate limiting at transport level
5. **Isolation**: No shared state between connections

## Future Directions

- **QUIC Transport**: For better mobile performance
- **Compression**: Optional compression for large messages
- **Streaming**: True bidirectional streaming for updates
- **WebRTC**: For voice/video relay servers