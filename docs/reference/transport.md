# Transport

TLRPC separates **carrier transports** (TCP / WebSocket) from **MTProto transport protocols** (Abridged / Intermediate / Padded Intermediate / Full). Carrier transports provide a byte stream; MTProto transport protocols frame MTProto payloads inside that stream.

## Interfaces

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
    SetDeadline(time.Time) error
    Context() context.Context
}
```

## MTProto Transport Protocols

Supported protocols:
- Abridged
- Intermediate
- Padded Intermediate
- Full

These are negotiated per-connection by inspecting the initial bytes (or, when obfuscation is used, the embedded protocol tag). Packet boundaries and length semantics come from the MTProto transport protocol codec, not from WebSocket/TCP frames.

## Obfuscation

`obfuscated2` (AES-CTR) is supported. It is optional for TCP and **required** for WebSocket connections. The 64‑byte obfuscation header carries the embedded transport protocol tag.

## Telegram Client Compatibility

- **TCP**: Abridged, Intermediate, Padded Intermediate, and Full are supported.
- **WebSocket**: `Sec-WebSocket-Protocol: binary` is required; frames are treated as a **byte stream** with MTProto framing inside; obfuscation is **required**.
- **Quick ack**: standalone quick-ack tokens are parsed for MTProto transports that support them.

## Built-in Implementations

- TCP transport (`transport.TCPTransport`)
- WebSocket transport (`transport.WebSocketTransport`)

Use `Server.ServeTransport` with any `transport.Listener` to accept MTProto connections.
