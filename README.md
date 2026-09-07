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
Framework consumers that need a read-only process-local snapshot of live
push-reachable users can call `Server.ActiveUserIDs()`, which returns a sorted,
detached slice of positive user IDs currently present in Runtime v2's push
registry.

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

To generate one package that serves every selected layer, opt in with
`--layers`. The list must contain the base and every supplied difference in
strictly increasing order. The base is the complete accepted baseline. It may
include older forms of a method by placing `// @tlrpc variant-layer <N>`
immediately before each historical declaration; the canonical declaration is
left unannotated. The annotation must be positive and no greater than the base
layer. It controls generated name provenance only; accepted baseline forms are
available from the generated package's base layer upward. This is the package's
supported floor, not a universal TL protocol minimum.

```bash
tlrpc-gen \
  --schema=./schema/base.tl \
  --base-layer=228 \
  --layers=228,229 \
  --layer-diff=229:./schema/layers/229.tl \
  --out=./gen \
  --package=gen
```

Multi-layer output keeps unchanged definitions and base-layer Go names once.
Changed request contracts get typed `Layer<N>` methods and disjoint descriptor
layer ranges. Additive object fields share one generated superset and are
encoded and decoded only in their declared layers. Other incompatible shapes
receive `Layer<N>` types. A type that is a union in any selected snapshot stays
one unsuffixed union containing its historical and current constructor
variants. Static shape propagation stops at that union boundary. The generated
constructor and method factories select same-ID wire variants by layer, and
`ProjectTLObject` recursively clones output objects for a target layer.
Hook-handled replacements re-enter automatic projection with the replacement
root hook skipped; nested hooks still run, so nested layer constraints remain
validated. A zero layer selects the base layer.

Single-layer generation remains unchanged. Runtime v2 records the client's
effective layer and uses generated descriptor and codec metadata when a
multi-layer package is registered; application semantics remain in typed
handlers and explicit projection hooks.

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
