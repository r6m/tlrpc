# TLRPC Architecture

## Overview

TLRPC is designed as a framework for building Telegram-compatible servers. It abstracts away MTProto complexity while maintaining full protocol compliance.

## Core Principles

1. **Framework, Not Server**: TLRPC provides building blocks, not a complete server
2. **Generated Code First**: All TL types are generated, not hand-written
3. **Layer Transparency**: Framework handles multi-layer clients automatically
4. **Zero-Copy Where Possible**: Minimize allocations in hot paths
5. **Pluggable Components**: Transport, session storage, crypto are replaceable

## Architecture Layers

```
┌─────────────────────────────────────┐
│         USER CODE                   │
│  Service Implementations            │
│  Business Logic                     │
└─────────────┬───────────────────────┘
              │ RegisterService()
┌─────────────▼───────────────────────┐
│      TLRPC FRAMEWORK                │
│  ┌─────────────────────────────┐    │
│  │    Service Registry         │    │
│  │  - Method routing           │    │
│  │  - Interceptor chain        │    │
│  └─────────────────────────────┘    │
│  ┌─────────────────────────────┐    │
│  │    Layer Adapter            │    │
│  │  - Per-layer serialization  │    │
│  │  - Constructor routing      │    │
│  └─────────────────────────────┘    │
│  ┌─────────────────────────────┐    │
│  │    MTProto Protocol         │    │
│  │  - Message framing          │    │
│  │  - Acknowledgments          │    │
│  │  - Resend logic             │    │
│  └─────────────────────────────┘    │
│  ┌─────────────────────────────┐    │
│  │    Session Manager          │    │
│  │  - Auth key storage         │    │
│  │  - Session state            │    │
│  │  - Layer tracking           │    │
│  └─────────────────────────────┘    │
│  ┌─────────────────────────────┐    │
│  │    Crypto Engine            │    │
│  │  - AES-256-IGE              │    │
│  │  - Auth key derivation      │    │
│  └─────────────────────────────┘    │
│  ┌─────────────────────────────┐    │
│  │    Transport                │    │
│  │  - TCP/UDP/WebSocket        │    │
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

**Features**:
- Message ID generation (time-based)
- Sequence numbers for ordering
- Acknowledgment tracking
- Resend requests

### 4. Layer Adapter

**Responsibility**: Handle different client layer versions

**Design**:
- Each layer has its own generated serializer
- Constructor ID → Type mapping per layer
- No automatic upgrade/downgrade (user handles in service)
- Layer exposed to service via context

```go
type Layer interface {
    Version() int
    Deserialize(constructorID uint32, data []byte) (TLObject, error)
    Serialize(obj TLObject) ([]byte, error)
    GetConstructorID(obj TLObject) uint32
}
```

### 5. Service Registry

**Responsibility**: Route requests to implementations

**Registration**:
```go
type ServiceDesc struct {
    ServiceName string
    Methods     []MethodDesc
}

type MethodDesc struct {
    MethodName string
    Handler    HandlerFunc
}

func (s *Server) RegisterService(sd ServiceDesc, ss interface{})
```

**Routing**:
1. Extract method name from TL object
2. Find handler in registry
3. Build interceptor chain
4. Execute handler
5. Serialize response

## Data Flow

### Request Path

```
1. Transport receives encrypted bytes
2. Crypto decrypts → plaintext TL bytes
3. Protocol parses message header
4. Layer adapter deserializes body (per client layer)
5. Registry routes to service handler
6. Interceptors execute (auth, logging, etc.)
7. User service implementation executes
```

### Response Path

```
1. User service returns response object
2. Registry serializes (using client's layer)
3. Protocol frames message
4. Crypto encrypts
5. Transport sends
```

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