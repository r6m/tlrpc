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
    "github.com/r6m/tlrpc/codec"
    "github.com/r6m/tlrpc/gen"
)

type MyAuthService struct {
    gen.UnimplementedAuthServer
}

func (s *MyAuthService) SendCode(ctx context.Context, req *gen.SendCodeRequest) (*gen.AuthSentCode, error) {
    return &gen.AuthSentCode{ /* ... */ }, nil
}

func main() {
    registry := codec.NewRegistry()
    registry.RegisterConstructor((&gen.SendCodeRequest{}).ConstructorID(), func() tlrpc.TLObject {
        return &gen.SendCodeRequest{}
    })

    server := tlrpc.NewServer(
        tlrpc.WithCodec(codec.New(registry)),
    )
    gen.RegisterAuthServer(server, &MyAuthService{})

    lis, _ := net.Listen("tcp", ":443")
    _ = server.Serve(lis)
}
```

Note: the default handshake handler is a minimal stub (only `req_pq`), so production deployments should provide a full MTProto handshake or a custom handler.

## Features

- **Multi-Layer Support**: Handle clients from different Telegram versions seamlessly
- **Auto-Generated Code**: Generate Go types and service interfaces from TL schema
- **Codec Registry**: Constructor-based TL decoding/encoding via codec registry
- **Pluggable Transports**: TCP, UDP, WebSocket support
- **Interceptor Chain**: Middleware for logging, auth, metrics
- **Type Safety**: Full compile-time type safety for all TL types
- **Performance**: Zero-allocation hot paths, efficient serialization

## Architecture

TLRPC follows a layered architecture:

1. **Transport Layer**: Handles raw connections (TCP/UDP/WS)
2. **Crypto Layer**: MTProto encryption/decryption
3. **Protocol Layer**: MTProto message framing and session management
4. **Codec (TL)**: Constructor registry and TL serialization
5. **Service Layer**: Your business logic implementation
