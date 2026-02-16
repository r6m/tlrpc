# TLRPC

**gRPC-like Framework for Telegram MTProto**

TLRPC is a gRPC-inspired framework for building Telegram-compatible RPC servers. Just as gRPC generates types and services from Protocol Buffers, TLRPC generates Go types and service interfaces from Telegram's TL schemas, providing a familiar RPC development experience for MTProto applications.

## Quick Start

```bash
# Install the code generator
go install github.com/r6m/tlrpc/cmd/tlrpc-gen@latest

# Generate service code from TL schema
tlrpc-gen --schema=layer222.tl --out=./gen
```

```go
package main

import (
    "context"
    "net"
    "log"

    "github.com/r6m/tlrpc"
    "github.com/r6m/tlrpc/gen"
)

type MyAuthService struct {
    gen.UnimplementedAuthServer
}

func (s *MyAuthService) SendCode(ctx context.Context, req *gen.SendCodeRequest) (*gen.AuthSentCode, error) {
    if req.PhoneNumber == "" {
        return nil, tlrpc.NewBadRequestError("PHONE_NUMBER_EMPTY")
    }

    return &gen.AuthSentCode{
        PhoneCodeHash: "abc123",
    }, nil
}

func main() {
    // Create the MTProto server
    server := tlrpc.NewServer()

    // Register your service implementation
    // (All internal registration happens automatically!)
    gen.RegisterAuthServer(server, &MyAuthService{})

    lis, _ := net.Listen("tcp", ":443")
    log.Println("Server listening on :443")
    log.Fatal(server.Serve(lis))
}
```

**Zero Configuration**: Unlike other frameworks, TLRPC handles all internal registration automatically. Just register your services - no manual codec setup or registry configuration required.

## How It Works

**gRPC Analogy:**
- **gRPC**: `proto` + service definitions → generate `ServiceDesc` + `Unimplemented*Server` → implement services → register with gRPC server
- **TLRPC**: `TL schema` + RPC definitions → generate `ServiceDesc` + `Unimplemented*Server` → implement services → register with TLRPC server

**Request Flow:**
```
Telegram Client → MTProto Transport → TLRPC Server → Your Service Implementation → Response → Client
```

## Features

- **gRPC-like Development**: Familiar service interface pattern with generated `ServiceDesc` and `Unimplemented*Server` stubs
- **Code Generation**: Auto-generate Go types and service interfaces from TL schemas
- **MTProto v2 Gateway**: Full MTProto protocol implementation for Android/web clients
- **Static Codec Dispatch**: Compile-time generated dispatch maps (no runtime registry like gRPC)
- **Interceptor Chain**: Middleware support for auth, logging, metrics (like gRPC interceptors)
- **Type Safety**: Compile-time type safety for all TL types
- **High Performance**: Efficient serialization and connection handling

## Architecture

TLRPC implements the complete MTProto stack as a gateway:

1. **Transport Layer**: TCP/WebSocket connections with MTProto framing
2. **Crypto Layer**: MTProto encryption/decryption with auth key management
3. **Protocol Layer**: Message serialization, session management, layer negotiation
4. **Dispatch Layer**: Automatic TL object dispatch via constructor ID mapping
5. **Service Layer**: Your business logic implementations (gRPC-like pattern)

## Testing

TLRPC includes comprehensive unit tests and integration tests for the MTProto v2 protocol implementation:

```bash
# Run all tests
go test ./...

# Run integration tests
go test -run TestFullMTProtoHandshake
```
