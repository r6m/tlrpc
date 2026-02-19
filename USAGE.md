# USAGE

## Installation

Install the generator and tools:

```bash
go install github.com/r6m/tlrpc/cmd/tlrpc-gen@latest
```

You can also install the client harness (optional):

```bash
go install github.com/r6m/tlrpc/cmd/tlrpc-client@latest
```

## Concepts

### TL objects (ConstructorID, SerializeTL/DeserializeTL)
TLRPC uses TL objects for all messages. A TL object must expose a constructor ID and be serializable.

```go
type TLObject interface {
	ConstructorID() uint32
}
```

Most generated types also implement:

- `SerializeTL(io.Writer) error`
- `DeserializeTL(io.Reader) error`
- `TLName() string`

### Methods + dispatch
Methods are TL objects with constructor IDs. The server registers constructors for decoding and a handler for each method:

```go
srv.RegisterConstructor(methodID, func() tlrpc.TLObject { return &MyRequest{} })
srv.RegisterMethod(methodID, func(ctx context.Context, obj tlrpc.TLObject) (interface{}, error) {
	return &MyResponse{}, nil
})
```

### Sessions/auth keys (high level)
Sessions are keyed by MTProto auth keys. Handshake negotiates the auth key; subsequent encrypted messages are associated with a session. The server attaches session and auth metadata to context for handlers.

## Quick start (no codegen)

This is a minimal hand-written TL method/response pair.

```go
package main

import (
	"context"
	"log"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/transport"
)

type PingRequest struct{}

func (p *PingRequest) ConstructorID() uint32 { return 0x7f010101 }
func (p *PingRequest) SerializeTL(w io.Writer) error {
	return mtproto.WriteUint32(w, p.ConstructorID())
}
func (p *PingRequest) DeserializeTL(r io.Reader) error { return nil }

type PongResponse struct{}

func (p *PongResponse) ConstructorID() uint32 { return 0x7f010102 }
func (p *PongResponse) SerializeTL(w io.Writer) error {
	return mtproto.WriteUint32(w, p.ConstructorID())
}
func (p *PongResponse) DeserializeTL(r io.Reader) error { return nil }

func main() {
	srv := tlrpc.NewServer()
	pingID := (&PingRequest{}).ConstructorID()
	
	srv.RegisterConstructor(pingID, func() tlrpc.TLObject { return &PingRequest{} })
	srv.RegisterMethod(pingID, func(ctx context.Context, obj tlrpc.TLObject) (interface{}, error) {
		return &PongResponse{}, nil
	})

	tcp := &transport.TCPTransport{AllowObfuscation: true}
	lis, err := tcp.Listen(":9000")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer func() { _ = lis.Close() }()

	log.Printf("listening on %s", lis.Addr())
	log.Fatal(srv.ServeTransport(lis))
}
```

## Codegen start (recommended)

### 1) Write a schema

Create a small schema:

```tl
---types---
echo.response#9f57e1e8 message:string = echo.Response;

---functions---
echo.echo#5e1f91a2 message:string = echo.Response;
```

### 2) Generate code

```bash
tlrpc-gen --schema examples/echo/schema.tl --out examples/echo/gen --package echo
```

### 3) Implement the generated server

Generated code includes an interface and a `Register...` helper:

```go
type EchoServer interface {
	Echo(ctx context.Context, req *EchoEchoRequest) (*EchoResponse, error)
}

func RegisterEchoServer(s *tlrpc.Server, srv EchoServer)
```

### 4) Register the service

```go
srv := tlrpc.NewServer()
echo.RegisterEchoServer(srv, &echoServer{})
```

## Registering methods and returning responses

Handlers return either:

- a concrete TL object pointer, or
- an interface for union types

The server will normalize and serialize the response correctly.

## Returning vectors

If a handler returns a slice, tlrpc encodes it as a TL vector automatically:

```go
func (s *svc) Users(ctx context.Context, req *gen.UsersGetUsersRequest) ([]gen.UserType, error) {
	return []gen.UserType{&gen.UserEmpty{ID: 1}}, nil
}
```

## Connection access and local push

Handlers can access the active connection and send a server-initiated message:

```go
func (s *svc) Echo(ctx context.Context, req *gen.EchoEchoRequest) (*gen.EchoResponse, error) {
	if conn, ok := tlrpc.ConnFromContext(ctx); ok {
		_ = conn.Send(&gen.EchoUpdate{Message: "push"})
	}
	return &gen.EchoResponse{Message: req.Message}, nil
}
```

Notes:
- `Conn.Send` is safe-by-default: tlrpc handles msg_id, seq_no, encryption, and writing.
- Use `Conn.Send` only for *local push* (current connection). It does not route to other sessions.

## Session lifecycle hooks (for external routing)

Register hooks to observe binds/unbinds. Use these to integrate external routing/presence (pseudocode only).

```go
srv := tlrpc.NewServer(
	tlrpc.WithOnSessionBound(func(binding tlrpc.Binding, conn tlrpc.Conn) {
		// e.g. publish presence or routing info
	}),
	tlrpc.WithOnSessionUnbound(func(binding tlrpc.Binding) {
		// e.g. cleanup routing state
	}),
)
```

### Redis presence (pseudocode)

```
OnSessionBound(binding, conn):
  redis.SET("presence:"+binding.UserID, "online")

OnSessionUnbound(binding):
  redis.DEL("presence:"+binding.UserID)
```

### NATS routing (pseudocode)

```
OnSessionBound(binding, conn):
  nats.Publish("routing.bind", binding)

OnSessionUnbound(binding):
  nats.Publish("routing.unbind", binding)
```

No Redis/NATS dependencies are included in tlrpc. Integrations belong in application code.

## Running the example

The repository includes a small echo server example:

```bash
go build ./examples/echo
./echo
```

It listens on TCP `:9000` and WS `:9001`. Use `tlrpc-client` or your own MTProto client to connect.

## Troubleshooting

- **AUTH_KEY_UNREGISTERED**: handshake not completed or auth key not stored.
- **METHOD_UNIMPLEMENTED**: method constructor ID is not registered.
- **LAYER_INVALID**: client requested a layer the server does not support.

## Non-goals

- tlrpc does not implement storage, dialogs, updates state/difference engines, or multi-node routing.
- tlrpc does not decide *who* to push to; applications must route updates explicitly.
