# Implementation

This guide documents the current public application model. Planned work is
listed only in [Roadmap](./roadmap.md).

## 1. Define and generate a schema

An application starts with its own TL schema:

```tl
---types---
echo.response#9f57e1e8 message:string = echo.Response;

---functions---
echo.echo#5e1f91a2 message:string = echo.Response;
```

Install and run the generator:

```bash
go install github.com/r6m/tlrpc/cmd/tlrpc-gen@latest
tlrpc-gen --schema=./schema.tl --out=./gen --package=gen
```

An application may also generate one target from a layered schema history:

```bash
tlrpc-gen \
  --schema=./schema/base.tl \
  --base-layer=226 \
  --layer-diff=227:./schema/layers/227.tl \
  --layer-diff=228:./schema/layers/228.tl \
  --layer=228 \
  --out=./gen \
  --package=gen
```

The inputs have distinct roles:

- `--schema` is the base schema file.
- `--base-layer` labels the layer represented by that base schema.
- `--layer-diff=<layer>:<path>` adds a layer delta. The flag is repeatable,
  and deltas are applied in the order they appear on the command line.
- `--layer` selects the target layer represented by the generated package.

Delta files contain ordinary TL declarations and section markers. A
constructor or function with the same declaration name replaces the previous
declaration in that domain; a previously unseen name adds a declaration. Use
only these exact comment directives for removals:

```tl
// @tlrpc remove constructor NAME
// @tlrpc remove function NAME
```

The generator applies the ordered deltas through the requested target,
validates the resolved schema, and records the target as generated
`SchemaLayer` metadata. Layer resolution ends at generation. Runtime v2 never
loads delta files or rewrites objects, fields, constructor IDs, requests,
responses, or method semantics between layers.

Generation produces schema-derived files for:

- concrete TL objects and union interfaces;
- function request objects;
- constructor constants and factories;
- serialization/deserialization codecs;
- typed server interfaces and unimplemented stubs;
- static service/method descriptors and `Register*Server` helpers.

No generated service is mandatory. A custom schema can be unlayered, use its
own version numbering, and contain no Telegram declarations.

## 2. Implement and register generated services

```go
type EchoService struct {
	gen.UnimplementedEchoServer
}

func (s *EchoService) Echo(
	ctx context.Context,
	req *gen.EchoEchoRequest,
) (*gen.EchoResponse, error) {
	if req.Message == "" {
		return nil, tlrpc.NewBadRequestError("MESSAGE_EMPTY")
	}
	return &gen.EchoResponse{Message: req.Message}, nil
}

server := tlrpc.NewServer(
	tlrpc.WithUnaryInterceptor(loggingInterceptor),
)
gen.RegisterEchoServer(server, &EchoService{})
```

Generated registration calls `Server.RegisterService` with a complete static
descriptor. Application code does not register raw method IDs, constructor
factories, or fallback callbacks. Runtime-owned MTProto controls and wrappers
also do not appear as generated services.

Unary interceptors are appropriate for logging, metrics, panic recovery,
coarse authorization, and request policy. Product workflows remain in service
implementations.

Handler errors implementing `RPCError`, or errors built with TLRPC helpers,
are encoded as correlated TL `rpc_error` results. Other errors are normalized
through the framework error conversion path.

## 3. Configure persistence and keys

```go
server := tlrpc.NewServer(
	tlrpc.WithAuthKeyManager(authKeys),
	tlrpc.WithServerKeyManager(serverKeys),
	tlrpc.WithSessionStore(sessionStore),
)
```

`crypto.AuthKeyManager` stores permanent authorization keys.
`crypto.ServerKeyManager` supplies RSA keys used by the handshake.
`session.Store` persists detached Runtime v2 snapshots:

```go
type Store interface {
	Load(context.Context, session.SessionKey) (session.Snapshot, error)
	LoadOrCreate(
		context.Context,
		session.SessionKey,
		session.Snapshot,
	) (snapshot session.Snapshot, created bool, err error)
	Save(context.Context, session.SessionKey, session.Snapshot) error
	Delete(context.Context, session.SessionKey) error
}
```

The key is the complete `(AuthKeyID, SessionID)` identity. Store methods must
be concurrency-safe, context-aware, and durably complete before returning
success. Values must be detached: retaining a mutable runtime pointer is not a
valid persistence implementation.

`session.Snapshot` contains named protocol fields for salt, layer, client
metadata, user binding, independent inbound/outbound sequence progress,
replay-window message IDs, new-session progress, and timestamps. Persist every
field. Application domain data belongs in a separate application store.

`session.NewMemoryStore` is suitable for tests and single-process development,
not durable production operation. Auth-key material must be encrypted at rest
and never logged.

## 4. Serve TCP and WebSocket

```go
tcp := &transport.TCPTransport{AllowObfuscation: true}
tcpListener, err := tcp.Listen(":9000")
if err != nil {
	return err
}

ws := &transport.WebSocketTransport{}
wsListener, err := ws.Listen(":9001")
if err != nil {
	return err
}

go func() { _ = server.ServeTransport(tcpListener) }()
go func() { _ = server.ServeTransport(wsListener) }()
```

TCP supports abridged, intermediate, padded-intermediate, and full MTProto
framing. WebSocket is treated as a continuous byte stream, requires the
`binary` subprotocol and obfuscated2, and feeds the same Runtime v2 framing and
decode path.

Each accepted physical connection is pinned to one auth key and admits up to
16 same-auth composite sessions by default. Every session has independent
lease, validator, reliability, router, active-request registry, writer, and
push-subscription state. Request admission is connection-wide. Session writers
own per-session protocol ordering and sequence progress, then submit complete
frames through one serialized connection-owned sink. Closing or replacing one
session retires only that session and never closes the shared transport;
connection shutdown owns transport closure.

`transport.Conn.ReadMessage(maxPayloadBytes)` is the bounded packet-read
contract. Framing codecs check declared lengths and configured/hard ceilings
before allocation. Nested encrypted, TL, vector, container, and compressed
inputs are bounded again during protocol decoding.

`Server.Stop` closes owned listeners and active connections, cancels work, and
waits for owned connection goroutines. Repeated calls are safe.

## 5. Server configuration

The current server surface is intentionally narrow:

```go
func NewServer(...ServerOption) *Server
func (*Server) RegisterService(ServiceDesc, interface{})
func (*Server) Serve(net.Listener) error
func (*Server) ServeTransport(transport.Listener) error
func (*Server) Publish(userID int64, object TLObject) error
func (*Server) PublishContext(context.Context, userID int64, object TLObject) error
func (*Server) PublishExcept(userID int64, excluded Binding, object TLObject) error
func (*Server) PublishExceptContext(context.Context, userID int64, excluded Binding, object TLObject) error
func (*Server) Stop() error
```

Current options are:

```text
WithUnaryInterceptor
WithSessionStore
WithAuthKeyManager
WithServerKeyManager
WithLogger
WithOnSessionBound
WithOnSessionUnbound
WithReliabilityLimits
WithResourceLimits
```

`WithResourceLimits` accepts one Runtime v2 policy rather than separate
gRPC-shaped message, stream, and timeout knobs:

```go
tlrpc.WithResourceLimits(tlrpc.ResourceLimits{
	MaxPayloadBytes:     16 << 20,
	MaxInFlightRequests: 1024,
	ReadTimeout:         2 * time.Minute,
	WriteTimeout:        30 * time.Second,
})
```

Every supplied field must be positive; invalid policies panic during
configuration. Payload size is enforced before transport allocation and before
protocol decode. Request admission, reliability capacity/TTL, and directional
deadlines are bounded. `NewServer` applies the values shown above by default;
the option replaces that complete policy. Custom `transport.Conn`
implementations must expose independent `SetReadDeadline` and
`SetWriteDeadline` operations.

There is no compatibility runtime selector or deprecated option family.

## 6. Handler context and binding

Handlers can read immutable runtime metadata:

```go
layer := tlrpc.LayerFromContext(ctx)
authKeyID := tlrpc.AuthKeyIDFromContext(ctx)
userID := tlrpc.UserIDFromContext(ctx)
client, hasClient := tlrpc.ClientMetadataFromContext(ctx)
binding, bound := tlrpc.BindingFromContext(ctx)
```

`Binding` contains the auth-key ID, session ID, server salt, user ID, and layer
observed for the request. It is a value snapshot, not a mutable protocol
object.

An authentication service requests a user-binding mutation explicitly:

```go
if err := tlrpc.BindSessionUser(ctx, userID); err != nil {
	return nil, err
}
```

`UnbindSessionUser(ctx)` clears the application user binding. Runtime v2
persists a successful mutation before publishing the changed presence. Failed
handlers do not commit collected binding mutations.

Handlers cannot retrieve a mutable session or raw network connection. That
boundary prevents application code from changing sequence numbers, salts,
message IDs, encryption, reliability records, or write ordering.

## 7. Semantic server push

Send a schema-defined object on the current request's connection:

```go
sender, ok := tlrpc.SenderFromContext(ctx)
if ok {
	err := sender.Send(ctx, update)
}
```

Send to every locally active subscribed session bound to an application user:

```go
err := server.Publish(userID, update)

// Use PublishContext when delivery should inherit a deadline or cancellation.
err = server.PublishContext(ctx, userID, update)

// Publish to the user's other local sessions, excluding the exact protocol
// session that originated the change.
binding, ok := tlrpc.BindingFromContext(ctx)
if ok {
	err = server.PublishExceptContext(ctx, userID, binding, update)
}
```

Publish exclusion compares only `Binding.AuthKeyID` and `Binding.SessionID`.
Sessions that share just one of those values are still included.

All publish paths submit semantic push to the target composite session's
writer. That writer owns the session's protocol ordering and submits complete
encrypted frames through the connection-owned serialized frame sink. The
application does not allocate IDs, wrap containers, encrypt, or write transport
frames.

`invokeWithoutUpdates` is composite-session-local. For that wrapped invocation,
asynchronous push is suppressed for the session while RPC execution and its
correlated response continue. Other sessions on the same physical connection
or elsewhere are never affected.

`Server.Publish` is process-local and best-effort. It is not a durable update
log, recipient engine, outbox, or cross-node fanout service. Commit application
update state before attempting live delivery.

Lifecycle hooks observe immutable binding values. The bound hook also receives
a semantic `Sender`; it does not receive a raw connection.

## 8. Verification

Framework changes should verify these axes separately:

- generate, compile, register, and dispatch the custom non-Telegram acceptance
  schema;
- deterministically generate the exact Telegram layer-228 fixture;
- run malformed framing/decode and Runtime v2 unit tests;
- run TCP and WebSocket handshake/encrypted conformance;
- run reconnect, mixed push/RPC/control, ACK, state, resend, drop-answer,
  cancellation, and shutdown tests;
- run independent gotd compatibility where required;
- run architecture checks that reject legacy APIs and physical writes outside
  the Runtime v2 connection frame sink.

Run broad Go verification sequentially (for example with `go test -p=1`) to
avoid creating competing compiler fleets on development machines.

## 9. Current limitations

- Local publish is not durable or distributed.
- v0.8.0 is published and `tgserver` verifies its source cutover against that
  module without a workspace replacement.
