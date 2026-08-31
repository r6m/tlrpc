# tlrpc

TLRPC is a generic, TL-schema-first RPC framework for Go servers. An
application supplies its own TL schema, generates typed Go objects and
gRPC-like service contracts, implements those service interfaces, and
registers them with a TLRPC server. The wire protocol is TL/MTProto; protobuf
and gRPC are not dependencies or hidden companion protocols.

`tgserver` is one consumer of the framework, not its purpose or a required
companion. Telegram layer 228 is a large compatibility fixture used to prove
the parser, generator, and runtime against a real schema.

## Model

```text
application-owned .tl schema
  -> tlrpc-gen
  -> generated types, codecs, service interfaces, descriptors, registration
  -> application service implementations
  -> Runtime v2 over TCP or WebSocket
  -> TL/MTProto clients
```

Runtime v2 owns the reusable protocol edge: framing, authorization-key
handshake, encryption, composite sessions, validation, wrappers, containers,
controls, request correlation, bounded writes, and live process-local push.
The application owns its API schema, service semantics, authentication policy,
durable domain data, durable update recovery, and deployment configuration.

## Generate and serve

```bash
go install github.com/r6m/tlrpc/cmd/tlrpc-gen@latest
tlrpc-gen --schema=./schema.tl --out=./gen --package=gen
```

For projects that maintain schema differences, generation can resolve a base
schema to one selected target layer:

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

Layer awareness ends at generation. Runtime v2 records the client's declared
layer but never selects another generated package or translates constructors,
fields, or method semantics between layers.

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

Generated `Register*Server` helpers are the application dispatch surface.
Handlers receive typed requests and immutable context metadata; they do not
receive mutable protocol sessions or raw connections.

## Production-readiness surface

The v0.12.0 Production Readiness release adds or completes:

- exact `invokeAfterMsg`/`invokeAfterMsgs` dependency ordering with bounded
  completion history and Telegram-compatible wait errors;
- durable replay protection through the session snapshot's client message-ID
  floor, recent message IDs, and recent content sequence numbers;
- shared per-request decode budgets for bytes, wrappers, containers, vectors,
  object nodes/depth, and gzip work/expansion, plus bounded response encoding;
- connection, IP, auth-key, session, handler, and physical-write limits;
- non-blocking typed observer events for connections, handshakes, sessions,
  RPCs, admission, writes, stores, and gauges;
- explicit WebSocket origin policy, bounded HTTP upgrade admission, and the
  required `binary` subprotocol; and
- RSA private-key loading restricted to regular owner-only files, with saved
  keys forced to mode `0600`.

See [docs/implementation.md](./docs/implementation.md) for the exact public
configuration and defaults.

## Documentation

Start at [docs/index.md](./docs/index.md):

- [Requirements](./docs/requirements.md)
- [Architecture](./docs/architecture.md)
- [Implementation](./docs/implementation.md)
- [Telegram and MTProto](./docs/telegram-mtproto.md)
- [Roadmap](./docs/roadmap.md)

## Release status

v0.12.0 is the current Production Readiness release. TLRPC is still pre-1.0:
minor releases may deliberately replace unfinished APIs instead of preserving
legacy adapters. See [CHANGELOG.md](./CHANGELOG.md).
