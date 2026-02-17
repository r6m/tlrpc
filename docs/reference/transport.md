# Transport

Transport abstraction is in `transport` package.

```go
type Transport interface {
    Listen(addr string) (Listener, error)
    Dial(addr string) (Conn, error)
}

type Listener interface {
    Accept() (Conn, error)
    Close() error
    Addr() net.Addr
}

type Conn interface {
    ReadMessage() ([]byte, error)
    WriteMessage([]byte) error
    Close() error
    LocalAddr() net.Addr
    RemoteAddr() net.Addr
    Context() context.Context
}
```

Built-in implementations:

- TCP transport
- WebSocket transport
- Obfuscated transport support primitives

Runtime framing in `conn.go`/transport is length-prefixed and aligned for MTProto packet handling.
