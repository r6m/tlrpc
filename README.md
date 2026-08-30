# tlrpc

TLRPC is a standalone, schema-first Go RPC framework for TL. A project supplies
its own TL schema, generates typed Go objects and gRPC-like service contracts,
implements those services, and registers them with a TLRPC server. The public
wire protocol remains TL/MTProto—protobuf and gRPC are not involved.

Runtime v2 provides the reusable protocol edge: bounded TCP and WebSocket
framing, MTProto authorization-key handshake and encryption, and wrapper and
control handling. Each physical connection is pinned to one auth key and hosts
a bounded map of same-auth composite sessions (16 by default). Every session
independently owns its lease, validation, reliability, routing, active-request
registry, writer, and push subscription. Session writers own protocol ordering,
message IDs, sequence numbers, correlation, encryption, and persistence; one
connection-owned, serialized frame sink performs physical writes without
letting a session writer close the transport. Request admission is bounded
across the connection, and lease replacement retires only the matching session.

The application owns its schema, service behavior, authorization policy,
durable domain state, and deployment configuration. Telegram layer 228 is a
compatibility fixture used to prove the parser, generator, and MTProto runtime;
it is not a framework default or a baked-in product. `tgserver` is a separate
consumer and integration proof, not a required companion.

## Quick start

```bash
go install github.com/r6m/tlrpc/cmd/tlrpc-gen@latest
tlrpc-gen --schema=./schema.tl --out=./gen --package=gen
```

For a layered schema, provide the base layer, repeat `--layer-diff` in the
order the fragments must be applied, and select one target layer:

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

Each delta fragment contains ordinary TL declarations. A declaration with the
same name replaces the previous declaration, a new name adds a declaration,
and the exact comment directives `// @tlrpc remove constructor NAME` and
`// @tlrpc remove function NAME` remove declarations. Resolution happens only
during generation; Runtime v2 never converts objects or method semantics
between layers.

```go
type EchoService struct {
	gen.UnimplementedEchoServer
}

func (s *EchoService) Echo(
	ctx context.Context,
	req *gen.EchoEchoRequest,
) (*gen.EchoResponse, error) {
	return &gen.EchoResponse{Message: req.Message}, nil
}

server := tlrpc.NewServer()
gen.RegisterEchoServer(server, &EchoService{})
log.Fatal(server.Serve(listener))
```

Generated registration is the application dispatch surface. Applications do
not register raw constructor or method callbacks and handlers are not given a
mutable protocol session or raw connection.

## Documentation

Start at [docs/index.md](./docs/index.md):

- [Requirements](./docs/requirements.md)
- [Architecture](./docs/architecture.md)
- [Implementation](./docs/implementation.md)
- [Telegram and MTProto](./docs/telegram-mtproto.md)
- [Roadmap](./docs/roadmap.md)

The documentation labels current Runtime v2 behavior separately from future
work and from the later `tgserver` integration.

## Release status

v0.8.0 is the first supported Runtime v2 framework release. Earlier APIs have
no backward-compatibility contract: superseded APIs and the old runtime were
replaced rather than retained as legacy modes. See [CHANGELOG.md](./CHANGELOG.md)
for release notes.
