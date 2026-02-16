# TLRPC Tutorial: Building a gRPC-like MTProto Server

This tutorial shows how to build a Telegram-compatible RPC server using TLRPC. If you're familiar with gRPC, you'll find the development workflow very similar - just replace Protocol Buffers with TL schemas.

## Prerequisites

- Go 1.24+
- Basic understanding of RPC concepts (like gRPC)
- Familiarity with Telegram's TL schema format

## The Big Picture

**TLRPC is to MTProto what gRPC is to Protocol Buffers:**

- **gRPC**: `.proto` files → `protoc` → generated types/services → implement services → register with server
- **TLRPC**: `.tl` files → `tlrpc-gen` → generated types/services → implement services → register with server

You're building an **MTProto gateway** that accepts requests from Telegram clients (Android, web, etc.) and routes them to your service implementations.

## Step 1: Set up your project

Create a new Go module (just like any Go project):

```bash
mkdir my-telegram-server
cd my-telegram-server
go mod init my-telegram-server
```

## Step 2: Add TLRPC dependency

```bash
go get github.com/r6m/tlrpc
```

## Step 3: Create your TL schema

Create a `schema.tl` file defining your RPC services (similar to `.proto` files):

```
---types---
user#8f97c628 id:long first_name:string = User;

---functions---
getUser#12345678 user_id:long = User;
auth.sendCode#12345679 phone_number:string = auth.SentCode;
```

This defines:
- A `User` type with `id` and `first_name` fields
- Two RPC methods: `getUser` and `auth.sendCode`

## Step 4: Generate Go code

Install and run the code generator (like `protoc` for gRPC):

```bash
go install github.com/r6m/tlrpc/cmd/tlrpc-gen@latest
tlrpc-gen --schema=schema.tl --out=./gen --package=gen
```

This generates:
- Go types for your TL objects (`User`, `GetUserRequest`, etc.)
- Service interfaces (`UserServer`, `AuthServer`)
- Unimplemented stubs (`UnimplementedUserServer`, `UnimplementedAuthServer`)
- Registration helpers (`RegisterUserServer`, `RegisterAuthServer`)
- Codec registration (`RegisterCodec`)

## Step 5: Implement your services

Implement your services by embedding the generated stubs (just like gRPC):

```go
package main

import (
    "context"
    "my-telegram-server/gen"
)

type UserService struct {
    gen.UnimplementedUserServer // Embed the generated stub
}

func (s *UserService) GetUser(ctx context.Context, req *gen.GetUserRequest) (*gen.User, error) {
    // Your business logic here - this could call a database, etc.
    return &gen.User{
        ID:        req.UserID,
        FirstName: "John Doe",
    }, nil
}

type AuthService struct {
    gen.UnimplementedAuthServer
}

func (s *AuthService) SendCode(ctx context.Context, req *gen.SendCodeRequest) (*gen.AuthSentCode, error) {
    if req.PhoneNumber == "" {
        return nil, tlrpc.NewBadRequestError("PHONE_NUMBER_EMPTY")
    }

    // Send SMS code, etc.
    return &gen.AuthSentCode{
        PhoneCodeHash: "abc123",
    }, nil
}
```

## Step 6: Set up the MTProto server

Create the main server with codec and service registration:

```go
package main

import (
    "log"
    "net"

    "github.com/r6m/tlrpc"
    "github.com/r6m/tlrpc/codec"
    "my-telegram-server/gen"
)

func main() {
    // Set up the TL codec registry
    registry := codec.NewRegistry()
    gen.RegisterCodec(registry) // Registers all your TL constructors

    // Create the MTProto server
    server := tlrpc.NewServer(
        tlrpc.WithCodec(codec.New(registry)),
        // Add interceptors like gRPC
        tlrpc.WithInterceptor(tlrpc.LoggingInterceptor(log.Default())),
    )

    // Register your service implementations (like gRPC service registration)
    gen.RegisterUserServer(server, &UserService{})
    gen.RegisterAuthServer(server, &AuthServer{})

    // Start listening for MTProto connections
    lis, err := net.Listen("tcp", ":443")
    if err != nil {
        log.Fatal(err)
    }

    log.Println("MTProto server listening on :443")
    log.Fatal(server.Serve(lis))
}
```

## Step 7: Test with Telegram clients

Your server now accepts connections from:
- Official Telegram Android/iOS clients
- Web Telegram clients
- Any MTProto-compatible client

**Request Flow:**
```
Telegram Client → MTProto Connection → TLRPC Server → Your Service Method → Response → Client
```

## Advanced Features

### Adding Authentication

Use interceptors for auth (like gRPC):

```go
server := tlrpc.NewServer(
    tlrpc.WithCodec(codec.New(registry)),
    tlrpc.WithInterceptor(
        tlrpc.ChainInterceptors(
            tlrpc.LoggingInterceptor(log.Default()),
            tlrpc.AuthInterceptor([]string{"auth.sendCode"}), // Allow unauthenticated
        ),
    ),
)
```

### Session Management

Access user session data in your handlers:

```go
func (s *UserService) GetUser(ctx context.Context, req *gen.GetUserRequest) (*gen.User, error) {
    userID := tlrpc.UserIDFromContext(ctx)
    if userID == 0 {
        return nil, tlrpc.ErrUnauthorized
    }
    // Use userID for authorization...
}
```

### Multiple Services

Add more services as your API grows:

```go
// schema.tl additions
---functions---
messages.sendMessage#12345680 peer:InputPeer message:string = messages.Message;
channels.createChannel#12345681 title:string about:string = Channel;

// Generated services
gen.RegisterMessagesServer(server, &MessagesService{})
gen.RegisterChannelsServer(server, &ChannelsService{})
```

## Production Considerations

- **Full MTProto Handshake**: The default handshake only handles `req_pq`. For production, implement complete DH key exchange.
- **Session Storage**: Use persistent session storage (Redis, database) instead of memory.
- **Rate Limiting**: Add interceptors for rate limiting.
- **Metrics**: Add Prometheus metrics interceptors.
- **TLS**: Run behind a TLS terminator for production.

## Next Steps

- Study the [generated code](gen/) to understand the full API
- Read the [main documentation](../docs/tlrpc.md) for advanced features
- Check out [example implementations](../examples/) for complete servers
- Deploy behind a load balancer for production scaling
