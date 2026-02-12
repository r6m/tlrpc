# TLRPC Core Framework

The `tlrpc` package provides the core framework for building Telegram RPC servers and clients. It implements the MTProto protocol with layer support for handling different Telegram client versions.

## Overview

This package provides:
- **Server**: RPC server with automatic layer handling
- **Transport**: Pluggable transport abstractions (TCP, WebSocket, etc.)
- **Session Management**: Session storage and lifecycle management
- **Layer Support**: Automatic handling of different Telegram protocol layers
- **Interceptors**: Middleware for request/response processing
- **Type Safety**: Generated type-safe interfaces from TL schemas

## Quick Start

```go
package main

import (
    "context"
    "log"
    "net"

    "github.com/r6m/tlrpc"
    "github.com/r6m/tlrpc/gen" // generated from TL schema
)

// Implement your service
type AuthService struct {
    gen.UnimplementedAuthServer
}

func (s *AuthService) SendCode(ctx context.Context, req *gen.SendCodeRequest) (*gen.AuthSentCode, error) {
    return &gen.AuthSentCode{
        PhoneCodeHash: "abc123",
    }, nil
}

func main() {
    // Create server
    server := tlrpc.NewServer(
        tlrpc.WithLayers(195, 196, 197, 198, 199, 200, 201, 202,
                         203, 204, 205, 206, 207, 208, 209, 210),
    )

    // Register service
    gen.RegisterAuthServer(server, &AuthService{})

    // Listen
    lis, err := net.Listen("tcp", ":443")
    if err != nil {
        log.Fatal(err)
    }

    log.Println("Server listening on :443")
    server.Serve(lis)
}
```

## Architecture

The framework follows a layered architecture:

- **Transport Layer**: Handles network connections (TCP, WebSocket, etc.)
- **Protocol Layer**: MTProto message framing and encryption
- **Layer Layer**: Per-version type serialization and constructor routing
- **Service Layer**: Generated RPC service implementations
- **Session Layer**: Authentication and session management

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

## Layer Support

The framework automatically handles different Telegram protocol layers. Each layer version has its own serialization format and constructor IDs. The server can support multiple layers simultaneously.

## Error Handling

Standard RPC errors:

```go
var (
    ErrUnauthorized  = errors.New("tlrpc: unauthorized")
    ErrInvalidLayer  = errors.New("tlrpc: invalid layer")
    ErrMethodNotFound = errors.New("tlrpc: method not found")
)

type RPCError struct {
    Code    int
    Message string
}
```

## Interceptors

Middleware for request/response processing:

```go
type Handler func(ctx context.Context, req interface{}) (interface{}, error)
type Interceptor func(next Handler) Handler

func ChainInterceptors(interceptors ...Interceptor) Interceptor
```

## Context Helpers

Extract information from request contexts:

```go
func SessionFromContext(ctx context.Context) *Session
func LayerFromContext(ctx context.Context) int
func AuthKeyIDFromContext(ctx context.Context) int64
func UserIDFromContext(ctx context.Context) int64
```

## Code Generation

Services are generated from TL schemas using the `tlrpc-gen` tool:

```bash
tlrpc-gen --schema schema.tl --out gen/ --package gen
```

This generates:
- Type definitions with proper serialization
- Service interfaces (e.g., `AuthServer`)
- Unimplemented stubs (e.g., `UnimplementedAuthServer`)
- Registration helpers (e.g., `RegisterAuthServer`)
- Layer-specific implementations

## Codec

The server requires a `Codec` to decode/encode TL objects. Configure it with `tlrpc.WithCodec(...)`.

## Dependencies

This package depends on:
- `transport/`: Transport implementations
- `mtproto/`: MTProto protocol handling
- `layer/`: Layer abstraction
- `session/`: Session management
- `crypto/`: Cryptographic primitives
