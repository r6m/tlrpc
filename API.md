# TLRPC Public API Reference

## Core Types

### Server

```go
type Server struct { /* internal */ }

func NewServer(opts ...ServerOption) *Server
func (s *Server) RegisterService(desc ServiceDesc, impl interface{})
func (s *Server) Serve(lis net.Listener) error
func (s *Server) ServeTransport(lis Listener) error
func (s *Server) Stop() error
```

Note: `RegisterService` panics on invalid registrations (duplicate methods, invalid implementation). This mirrors grpc-style behavior and keeps startup failures explicit.

### grpc-style Conventions

- Register all services during startup; registration failures should be immediate and fatal.
- Invalid service descriptors or implementations trigger panics to avoid partial startup.
- Handler signatures are fixed and enforced by generated code, similar to grpc stubs.

### Server Options

```go
func WithTransport(t Transport) ServerOption
func WithMaxLayer(layer int) ServerOption
func WithLayers(layers ...int) ServerOption
func WithInterceptor(i Interceptor) ServerOption
func WithSessionStore(store SessionStore) ServerOption
func WithLogger(l Logger) ServerOption
```

### Context Functions

```go
func SessionFromContext(ctx context.Context) *Session
func LayerFromContext(ctx context.Context) int
func AuthKeyIDFromContext(ctx context.Context) int64
func UserIDFromContext(ctx context.Context) int64
```

### Interceptors

```go
type Handler func(ctx context.Context, req interface{}) (interface{}, error)
type Interceptor func(next Handler) Handler

func ChainInterceptors(interceptors ...Interceptor) Interceptor
```

### Service Descriptors

```go
type ServiceDesc struct {
    ServiceName string
    HandlerType interface{}
    Methods     []MethodDesc
}

type MethodDesc struct {
    MethodName string
    Handler    func(ctx context.Context, req interface{}) (interface{}, error)
}
```

### Errors

```go
var ErrUnauthorized = errors.New("tlrpc: unauthorized")
var ErrInvalidLayer = errors.New("tlrpc: invalid layer")
var ErrMethodNotFound = errors.New("tlrpc: method not found")

type RPCError struct {
    Code    int
    Message string
}

func (e *RPCError) Error() string
```

## Transport Interface

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

For concrete transports and framing, see `transport`.

## Session Interface

```go
type Session struct {
    ID        int64
    AuthKeyID int64
    Layer     int
    UserID    int64
    Data      map[string]interface{} // user-defined storage
}

type SessionStore interface {
    Get(authKeyID int64) (*Session, error)
    Save(session *Session) error
    Delete(authKeyID int64) error
}
```

## TLObject Interface

```go
type TLObject interface {
    ConstructorID() uint32
    Method() string // for RPC types
}
```

## Automatic Registration

TLRPC handles all internal registration automatically. Constructor IDs and method handlers are registered when you call `Register*Server()` functions. No manual codec or registry setup is required.

## Code Generation

### CLI: tlrpc-gen

```bash
tlrpc-gen [flags]

Flags:
  --schema string      Path to TL schema file (required)
  --out string         Output directory (default: ./gen)
  --package string     Go package name (default: gen)
  --layers string      Comma-separated layer versions
  --help               Show help
```

### Generated Code Structure

```
gen/
├── types.go          # All TL types
├── services.go       # Service interfaces
├── register.go       # Registration functions
└── layer/
    ├── layer195/     # Per-layer types (if multi-layer)
    ├── layer196/
    └── ...
```

## Example: Complete Service

```go
package main

import (
    "context"
    "log"
    "net"

    "github.com/r6m/tlrpc"
    "github.com/r6m/tlrpc/gen"
)

// Implement the generated interface
type AuthService struct {
    gen.UnimplementedAuthServer
    users UserStore
}

func (s *AuthService) SendCode(ctx context.Context, req *gen.SendCodeRequest) (*gen.AuthSentCode, error) {
    // Implementation
    return &gen.AuthSentCode{
        PhoneCodeHash: "abc123",
        // ...
    }, nil
}

func (s *AuthService) SignIn(ctx context.Context, req *gen.SignInRequest) (*gen.AuthAuthorization, error) {
    // Check code, create session
    return &gen.AuthAuthorization{
        User: user,
    }, nil
}

func main() {
    // Create server
    server := tlrpc.NewServer(
        tlrpc.WithLayers(195, 196, 197, 198, 199, 200, 201, 202,
                         203, 204, 205, 206, 207, 208, 209, 210),
        tlrpc.WithInterceptor(loggingInterceptor),
    )

    // Register service
    gen.RegisterAuthServer(server, &AuthService{
        users: NewUserStore(),
    })

    // Listen
    lis, err := net.Listen("tcp", ":443")
    if err != nil {
        log.Fatal(err)
    }

    log.Println("Server listening on :443")
    server.Serve(lis)
}

func loggingInterceptor(next tlrpc.Handler) tlrpc.Handler {
    return func(ctx context.Context, req interface{}) (interface{}, error) {
        log.Printf("Request: %T", req)
        resp, err := next(ctx, req)
        log.Printf("Response: %T, Error: %v", resp, err)
        return resp, err
    }
}
```
