# TLRPC Architecture Summary

**TLRPC is a gRPC-inspired framework for MTProto RPC servers.**

## Core Concept
- **TLRPC** = **gRPC for MTProto**
- **TL schemas** → `tlrpc-gen` → **generated types/services** → **familiar RPC patterns**
- **MTProto v2 gateway** for Telegram Android/web clients

## Architecture Overview

```
┌─────────────────┐
│   USER CODE     │  ← gRPC-like service implementations
│                 │     - Embed Unimplemented*Server
│   Business      │     - Implement RPC methods
│   Logic         │     - Register with TLRPC server
└─────────┬───────┘
          │ RegisterService()
┌─────────▼───────┐
│  TLRPC SERVER  │  ← RPC framework with interceptors
│                 │     - Service registry
│  Framework     │     - Method routing
│                 │     - Interceptor chain
└─────────┬───────┘
          │ MTProto Protocol
┌─────────▼───────┐
│PROTOCOL LAYERS │  ← MTProto v2 implementation
│                 │     - Transport (TCP/WebSocket)
│ MTProto v2     │     - Crypto (AES-IGE, RSA, DH)
│ Implementation │     - Session management
│                 │     - TL dispatch
└─────────────────┘
```

## Key Components

### 1. Code Generation (`tlrpc-gen`)
- **Input**: TL schema files (`.tl`)
- **Output**: Go types + service interfaces
- **Pattern**: Generates `Unimplemented*Server` (like gRPC)

### 2. Service Framework
- **Registration**: `Register*Server(server, &YourService{})`
- **Pattern**: Embed generated `Unimplemented*Server`
- **Interceptors**: Middleware chain (auth, logging, metrics)

### 3. MTProto Protocol Stack
- **Transport**: TCP/WebSocket connections
- **Crypto**: AES-256-IGE encryption, RSA/DH key exchange
- **Sessions**: Auth key management, state tracking
- **Dispatcher**: Automatic TL object dispatch via constructor ID mapping

## Request Flow

```
Telegram Client
    ↓ (MTProto message)
Transport Layer (TCP/WebSocket)
    ↓ (encrypted bytes)
MTProto Protocol (decrypt)
    ↓ (TL bytes)
Dispatcher (route by constructor ID)
    ↓ (with interceptors)
Your Service Implementation
    ↓ (business logic)
Response Object
    ↓ (serialize)
Dispatcher (encode)
    ↓ (encrypt)
MTProto Protocol
    ↓ (send)
Transport Layer
    ↓ (to client)
Telegram Client
```

## Development Pattern (gRPC-like)

```go
// 1. Define service in TL schema
---functions---
auth.sendCode#123456 phone:string = auth.SentCode;

// 2. Generate code
tlrpc-gen --schema=schema.tl --out=gen/

// 3. Implement service (like gRPC)
type AuthService struct {
    gen.UnimplementedAuthServer  // ← Embed generated stub
}

func (s *AuthService) SendCode(ctx context.Context, req *gen.SendCodeRequest) (*gen.AuthSentCode, error) {
    // Your business logic
    return &gen.AuthSentCode{PhoneCodeHash: "abc123"}, nil
}

// 4. Register and serve (like gRPC)
server := tlrpc.NewServer()
gen.RegisterAuthServer(server, &AuthService{}) // Automatic registration!
server.Serve(listener)
```

## Current Implementation Status

✅ **Working Components:**
- TL schema parsing and AST
- Go code generation (`tlrpc-gen`)
- MTProto serialization primitives
- Transport layer (TCP/WebSocket)
- Cryptographic primitives (AES-IGE, RSA, DH)
- Session management
- Automatic service registration and dispatch
- Complete MTProto v2 handshake (Diffie-Hellman key exchange)
- MTProto 2.0 message encryption with proper msg_key and KDF
- Production session storage backends
- Comprehensive examples and documentation

## Key Interfaces

```go
// Core server
type Server struct { /* framework implementation */ }

// Service registration (gRPC-like)
func (s *Server) RegisterService(desc ServiceDesc, impl interface{})

// Interceptors (gRPC-like middleware)
type Interceptor func(next Handler) Handler

// Transport abstraction
type Transport interface {
    Listen(addr string) (Listener, error)
    Dial(addr string) (Conn, error)
}
```

## Concurrency Model

- **Per-Connection Goroutines**: Each client gets dedicated goroutine
- **Thread-Safe**: Service dispatcher, sessions are thread-safe
- **Concurrent Handlers**: Multiple requests per session can execute concurrently
- **Ordering**: MTProto message IDs ensure proper ordering

## Error Handling

- **Transport Level**: Connection errors, timeouts
- **Protocol Level**: MTProto decryption, invalid messages
- **RPC Level**: Method not found, invalid parameters
- **Service Level**: Business logic errors (converted to RPC errors)

This architecture enables building Telegram-compatible servers with the same developer experience as gRPC, but with full MTProto v2 protocol support.