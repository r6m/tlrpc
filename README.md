# TLRPC

**Telegram RPC Framework for Go**

TLRPC is a high-performance framework for building Telegram servers. It handles the complexity of MTProto protocol, multi-layer support, and encryption, allowing you to focus on implementing business logic.

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

    "github.com/r6m/tlrpc"
    "github.com/r6m/tlrpc/gen"
)

type MyAuthService struct {
    gen.UnimplementedAuthServer
}

func (s *MyAuthService) SendCode(ctx context.Context, req *gen.SendCodeRequest) (*gen.AuthSentCode, error) {
    return &gen.AuthSentCode{ /* ... */ }, nil
}

func main() {
    server := tlrpc.NewServer()
    gen.RegisterAuthServer(server, &MyAuthService{})

    lis, _ := net.Listen("tcp", ":443")
    _ = server.Serve(lis)
}
```

## Features

- **Multi-Layer Support**: Handle clients from different Telegram versions seamlessly
- **Auto-Generated Code**: Generate Go types and service interfaces from TL schema
- **Pluggable Transports**: TCP, UDP, WebSocket support
- **Interceptor Chain**: Middleware for logging, auth, metrics
- **Type Safety**: Full compile-time type safety for all TL types
- **Performance**: Zero-allocation hot paths, efficient serialization

## Architecture

TLRPC follows a layered architecture:

1. **Transport Layer**: Handles raw connections (TCP/UDP/WS)
2. **Crypto Layer**: MTProto encryption/decryption
3. **Protocol Layer**: MTProto message framing and session management
4. **Layer Adapter**: Handles different client layer versions
5. **Service Layer**: Your business logic implementation
