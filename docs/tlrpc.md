# TLRPC Core Framework

The `tlrpc` package provides a gRPC-like framework for building Telegram-compatible RPC servers. It implements the complete MTProto v2 protocol stack, allowing developers to build Telegram server applications using familiar RPC patterns.

## Overview

**TLRPC is to MTProto what gRPC is to Protocol Buffers.**

Just as gRPC generates types and services from `.proto` files, TLRPC generates Go types and service interfaces from Telegram's TL schemas. The development workflow is intentionally similar to gRPC:

1. **Define your services** in TL schema files
2. **Generate code** using `tlrpc-gen`
3. **Implement services** by embedding generated `Unimplemented*Server` stubs
4. **Register services** with the TLRPC server
5. **Add interceptors** for cross-cutting concerns (auth, logging, metrics)

## Quick Start

```go
package main

import (
    "context"
    "log"
    "net"

    "github.com/r6m/tlrpc"
    "github.com/r6m/tlrpc/codec"
    "github.com/r6m/tlrpc/gen" // generated from TL schema
)

// Implement your service (just like gRPC)
type AuthService struct {
    gen.UnimplementedAuthServer
}

func (s *AuthService) SendCode(ctx context.Context, req *gen.SendCodeRequest) (*gen.AuthSentCode, error) {
    return &gen.AuthSentCode{
        PhoneCodeHash: "abc123",
    }, nil
}

func main() {
    // Set up codec registry (like gRPC service registration)
    registry := codec.NewRegistry()
    gen.RegisterCodec(registry)

    // Create server
    server := tlrpc.NewServer(
        tlrpc.WithCodec(codec.New(registry)),
    )

    // Register your service implementation
    gen.RegisterAuthServer(server, &AuthService{})

    // Start serving
    lis, err := net.Listen("tcp", ":443")
    if err != nil {
        log.Fatal(err)
    }

    log.Println("MTProto server listening on :443")
    log.Fatal(server.Serve(lis))
}
```

## Architecture

TLRPC implements the complete MTProto protocol stack as a gateway for Telegram clients:

- **Transport Layer**: TCP/WebSocket connections with MTProto message framing
- **Crypto Layer**: MTProto v2 encryption/decryption with auth key management
- **Protocol Layer**: Session management, message sequencing, layer negotiation
- **Codec Layer**: TL object serialization/deserialization via constructor registry
- **Service Layer**: Your business logic implementations (gRPC-like RPC pattern)

## Key Components

### Server

The main server type that orchestrates all components:

```go
type Server struct {
    // internal implementation
}

func NewServer(opts ...ServerOption) *Server
func (s *Server) RegisterService(desc ServiceDesc, impl interface{})
func (s *Server) Serve(lis net.Listener) error
func (s *Server) ServeTransport(lis Listener) error
func (s *Server) Stop() error
```

### Session Management

Sessions track authenticated users and their protocol state:

```go
type Session struct {
    ID        int64
    AuthKeyID int64
    Layer     int
    UserID    int64
    Data      map[string]interface{}
}

type SessionStore interface {
    Get(authKeyID int64) (*Session, error)
    Save(session *Session) error
    Delete(authKeyID int64) error
}
```

### Transport Abstraction

Pluggable transport layer for different network protocols:

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
}
```

## Code Generation (gRPC-like)

Services are generated from TL schemas using the `tlrpc-gen` tool, similar to how `protoc` generates gRPC code:

```bash
tlrpc-gen --schema schema.tl --out gen/ --package gen
```

This generates the same pattern as gRPC:
- **Type definitions** with TL serialization (`SerializeTL`/`DeserializeTL`)
- **Service interfaces** (e.g., `AuthServer`) - just like gRPC service interfaces
- **Unimplemented stubs** (e.g., `UnimplementedAuthServer`) - embed these in your implementations
- **Registration helpers** (e.g., `RegisterAuthServer`) - register your services with the server
- **Codec registry helper** (e.g., `RegisterCodec`) - registers TL constructors

## Service Implementation Pattern

Your service implementations follow the same pattern as gRPC:

```go
type MyAuthService struct {
    gen.UnimplementedAuthServer // Embed the generated stub
}

// Implement only the methods you need
func (s *MyAuthService) SendCode(ctx context.Context, req *gen.SendCodeRequest) (*gen.AuthSentCode, error) {
    // Your business logic here
    return &gen.AuthSentCode{PhoneCodeHash: "abc123"}, nil
}
```

## Codec & Registry

The `Codec` handles TL object encoding/decoding via a constructor registry:

```go
registry := codec.NewRegistry()
gen.RegisterCodec(registry) // Registers all generated constructors

server := tlrpc.NewServer(
    tlrpc.WithCodec(codec.New(registry)),
)
```

## Interceptors (gRPC-style Middleware)

Interceptors provide middleware functionality similar to gRPC interceptors:

```go
type Handler func(ctx context.Context, req interface{}) (interface{}, error)
type Interceptor func(next Handler) Handler

func ChainInterceptors(interceptors ...Interceptor) Interceptor
```

Built-in interceptors:
- **AuthInterceptor**: Enforces authentication requirements
- **LoggingInterceptor**: Logs all RPC calls
- **RecoveryInterceptor**: Recovers from panics
- **TimeoutInterceptor**: Adds request timeouts

## Context & Session Management

Access request context information (similar to gRPC metadata):

```go
func SessionFromContext(ctx context.Context) *Session
func LayerFromContext(ctx context.Context) int
func AuthKeyIDFromContext(ctx context.Context) int64
func UserIDFromContext(ctx context.Context) int64
```

## Error Handling

TLRPC uses MTProto-compatible RPC errors that are automatically serialized as TL objects:

```go
// Common MTProto errors
ErrBadRequest   = NewRPCError(400, "BAD_REQUEST")
ErrUnauthorized = NewRPCError(401, "UNAUTHORIZED")
ErrForbidden    = NewRPCError(403, "FORBIDDEN")
ErrNotFound     = NewRPCError(404, "NOT_FOUND")
ErrFlood        = NewRPCError(420, "FLOOD")
ErrInternal     = NewRPCError(500, "INTERNAL")

// Helper functions
NewBadRequestError(message string) *RPCError
NewUnauthorizedError(message string) *RPCError
NewForbiddenError(message string) *RPCError
NewNotFoundError(message string) *RPCError
NewFloodError(message string) *RPCError
NewInternalError(message string) *RPCError
```

When handlers return errors, TLRPC automatically:
1. Converts them to `RPCError` format
2. Serializes as TL object (constructor ID: `0x2144ca19`)
3. Sends encrypted response to client

## Internal Architecture

TLRPC implements the complete MTProto v2 protocol stack:

- **`transport/`**: TCP/WebSocket transport implementations
- **`mtproto/`**: MTProto message framing and low-level protocol
- **`codec/`**: TL object serialization/deserialization
- **`session/`**: Session management and storage
- **`crypto/`**: MTProto encryption primitives
- **`registry/`**: Service registration and method dispatch
