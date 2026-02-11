# Transport Layer

This package provides the transport layer abstraction for TLRPC, handling raw byte streams and connection management.

## Overview

The transport layer is responsible for:
- Establishing and managing network connections
- Reading and writing raw message bytes
- Abstracting different transport protocols (TCP, WebSocket, etc.)

## Interfaces

### Transport
```go
type Transport interface {
    Listen(addr string) (Listener, error)
    Dial(addr string) (Conn, error)
}
```

### Listener
```go
type Listener interface {
    Accept() (Conn, error)
    Close() error
}
```

### Conn
```go
type Conn interface {
    ReadMessage() ([]byte, error)
    WriteMessage([]byte) error
    Close() error
}
```

## Implementations

- `tcp`: Standard TCP transport with optional TLS
- `websocket`: WebSocket transport for web clients
- `obfuscated`: Telegram's obfuscated transport protocol

## Usage

Transport implementations are pluggable and can be swapped based on deployment requirements.