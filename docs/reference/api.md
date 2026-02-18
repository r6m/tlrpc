# API Reference

## Core Server

```go
func NewServer(opts ...ServerOption) *Server
func (s *Server) RegisterService(desc ServiceDesc, impl interface{})
func (s *Server) RegisterConstructor(id uint32, ctor func() TLObject)
func (s *Server) RegisterMethod(id uint32, h func(context.Context, TLObject) (interface{}, error))
func (s *Server) Serve(lis net.Listener) error
func (s *Server) ServeTransport(lis transport.Listener) error
func (s *Server) Stop() error
```

`ServeTransport` can be called multiple times with different listeners to serve multiple carrier transports (for example, TCP and WebSocket on different ports).

## Server Options

```go
func WithMaxLayer(layer int) ServerOption
func WithLayers(layers ...int) ServerOption
func WithUnaryInterceptor(i UnaryInterceptor) ServerOption
func WithInterceptor(i Interceptor) ServerOption // legacy adapter
func WithSessionStore(store SessionStore) ServerOption
func WithSessionManager(manager session.Manager) ServerOption
func WithAuthKeyManager(manager crypto.AuthKeyManager) ServerOption
func WithServerKeyManager(manager crypto.ServerKeyManager) ServerOption
func WithLogger(l Logger) ServerOption
```

`WithUnaryInterceptor` is the primary interceptor API. `WithInterceptor` exists for backward compatibility with legacy middleware shape.

## Interceptors

```go
type UnaryHandler func(ctx context.Context, req interface{}) (interface{}, error)

type UnaryInterceptor func(
    ctx context.Context,
    req interface{},
    info *UnaryServerInfo,
    handler UnaryHandler,
) (resp interface{}, err error)

func ChainUnaryInterceptors(interceptors ...UnaryInterceptor) UnaryInterceptor
```

Legacy interceptor APIs are still available:

```go
type Handler func(ctx context.Context, req interface{}) (interface{}, error)
type Interceptor func(next Handler) Handler
func ChainInterceptors(interceptors ...Interceptor) Interceptor
```

## Service Registration Types

```go
type ServiceDesc struct {
    ServiceName string
    HandlerType interface{}
    Methods     []MethodDesc
}

type MethodDesc struct {
    MethodName    string
    ConstructorID uint32
    NewRequest    func() TLObject
    Handler       interface{}
}
```

Generated `Register*Server` functions populate these descriptors and call `RegisterService`.
`RegisterService` binds methods into the per-server dispatcher using constructor IDs.

## TL Object Conventions

`TLObject` is the runtime dispatch unit:

```go
type TLObject interface {
    ConstructorID() uint32
}
```

Generated request structs implement:

- `ConstructorID() uint32`
- `Method() string`
- `SerializeTL(io.Writer) error`
- `DeserializeTL(io.Reader) error`

## Context Helpers

```go
func SessionFromContext(ctx context.Context) *Session
func LayerFromContext(ctx context.Context) int
func AuthKeyIDFromContext(ctx context.Context) int64
func UserIDFromContext(ctx context.Context) int64
func IncomingMD(ctx context.Context) (MD, bool)
func OutgoingMD(ctx context.Context) (MD, bool)
```
