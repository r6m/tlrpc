# TLRPC Architecture

## Overview

TLRPC is a gRPC-inspired framework for building Telegram-compatible RPC servers. It implements the complete MTProto v2 protocol stack while providing a familiar RPC development experience.

**Core Concept**: TLRPC is to MTProto what gRPC is to Protocol Buffers - a code generation and RPC framework for a specific protocol.

## Core Principles

1. **gRPC-like Developer Experience**: TL schemas → generated types/services → familiar RPC patterns
2. **MTProto Protocol Gateway**: Complete MTProto v2 implementation for Telegram clients
3. **Generated Code First**: All TL types and services are generated, not hand-written
4. **Layer Transparency**: Framework handles multi-layer Telegram clients automatically
5. **Pluggable Components**: Transport, session storage, crypto are replaceable

## Architecture Layers

TLRPC implements the complete MTProto stack as a gateway:

```
┌─────────────────────────────────────┐
│         USER CODE                   │
│  gRPC-like Service Implementations  │
│  Business Logic                     │
└─────────────┬───────────────────────┘
              │ Register*Server()
┌─────────────▼───────────────────────┐
│      TLRPC FRAMEWORK                │
│  ┌─────────────────────────────┐    │
│  │    Service Dispatcher       │    │
│  │  - Constructor ID routing   │    │
│  │  - Interceptor chain        │    │
│  │  - Automatic registration   │    │
│  └─────────────────────────────┘    │
│  ┌─────────────────────────────┐    │
│  │    MTProto Protocol         │    │
│  │  - Message framing          │    │
│  │  - Encryption/decryption    │    │
│  └─────────────────────────────┘    │
│  ┌─────────────────────────────┐    │
│  │    Session Manager          │    │
│  │  - Auth key storage         │    │
│  │  - Session state            │    │
│  └─────────────────────────────┘    │
│  ┌─────────────────────────────┐    │
│  │    Transport                │    │
│  │  - TCP/WebSocket            │    │
│  └─────────────────────────────┘    │
└─────────────────────────────────────┘
```

## Component Details

### 1. Transport Layer

**Responsibility**: Raw byte streams, connection management

**Interface**:
```go
type Transport interface {
    Listen(addr string) (Listener, error)
    Dial(addr string) (Conn, error)
}

type Listener interface {
    Accept() (Conn, error)
    Close() error
}

type Conn interface {
    ReadMessage() ([]byte, error)
    WriteMessage([]byte) error
    Close() error
}
```

**Implementations**:
- `TCPTransport`: Standard TCP with optional TLS
- `WebSocketTransport`: WebSocket for web clients
- `ObfuscatedTransport`: Telegram obfuscation

### 2. Crypto Layer

**Responsibility**: MTProto encryption, auth keys

**Key Types**:
- `AuthKey`: Permanent key from handshake
- `TempAuthKey`: Optional PFS keys
- `SessionKey`: Derived per-session keys

**Algorithms**:
- AES-256-IGE for message encryption
- RSA for initial handshake
- DH for key exchange

### 3. Protocol Layer

**Responsibility**: MTProto message format

**Message Types**:
- `UnencryptedMessage`: Handshake only
- `EncryptedMessage`: All RPC calls
- `Container`: Batched messages

`UnencryptedMessage` is routed through a handshake handler. The default handler implements the complete MTProto v2 Diffie-Hellman key exchange for production use.

**Features**:
- Message ID generation and validation (time-based, ±30s window)
- Sequence numbers for ordering
- Automatic acknowledgment sending (msgs_ack)
- Container message batching support
- MTProto 2.0 encryption with proper msg_key and KDF

### 4. Service Dispatcher

**Responsibility**: Route constructor IDs to handlers with automatic registration

**Design**:
- Constructor ID → handler mapping for direct dispatch
- Automatic registration when services are registered
- No manual codec or registry setup required

**Registration**:
```go
type ServiceDesc struct {
    ServiceName string
    Methods     []MethodDesc
}

type MethodDesc struct {
    MethodName string
    Handler    func(ctx context.Context, req interface{}) (interface{}, error)
}

func (s *Server) RegisterService(sd ServiceDesc, ss interface{})
```

**Routing**:
1. Extract constructor ID from message
2. Find handler directly by constructor ID
3. Build interceptor chain
4. Execute handler
5. Serialize response

## Request Flow (gRPC-like)

**Complete Flow**: Telegram Client → MTProto Transport → TLRPC Server → Your Service → Response

### Request Path
```
1. Telegram client sends MTProto message
2. Transport receives encrypted bytes
3. MTProto layer decrypts → plaintext TL bytes
4. Extract constructor ID and route directly to handler
5. Interceptors execute (auth, logging, metrics - like gRPC interceptors)
6. Your service implementation executes (gRPC-like RPC method)
```

### Response Path
```
1. Your service returns response object
2. Codec serializes to TL bytes
3. MTProto layer encrypts message
4. Transport sends to client
```

### gRPC Analogy
- **gRPC**: `Client → Transport → gRPC Server → Your Service Method → Response`
- **TLRPC**: `Telegram Client → MTProto Transport → TLRPC Server → Your Service Method → Response`

The key difference: TLRPC handles MTProto protocol complexity automatically.

## Concurrency Model

- **Per-Connection Goroutine**: Each client connection gets its own goroutine
- **Session Locking**: Session state protected by mutex or sharded locks
- **Handler Concurrency**: Handlers may be called concurrently for same session
- **Ordering Guarantees**: Message IDs ensure ordering, not handler execution

## Error Handling

**Levels**:
1. **Transport errors**: Connection closed, timeout
2. **Crypto errors**: Decryption failed, auth key invalid
3. **Protocol errors**: Bad message ID, invalid sequence
4. **RPC errors**: Method not found, invalid parameters
5. **Service errors**: Business logic failures

**Error Conversion**:
- Go errors → TL RPC errors (with error codes)
- Some errors close connection (crypto)
- Some errors return RPC error packet (service)

## Extension Points

Users can customize:

1. **Transport**: Implement `Transport` interface
2. **Session Storage**: Implement `SessionStore` interface
3. **Interceptors**: Add to interceptor chain
4. **Metrics**: Hook into events
5. **Logging**: Provide logger implementation

## Performance Considerations

- **Object Pooling**: Reuse buffers for serialization
- **Zero-Copy**: Avoid copying where possible (unsafe OK)
- **Sharding**: Shard session maps by auth key ID
- **Batching**: Support message containers
- **Async I/O**: Non-blocking where possible
