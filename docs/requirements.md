# Requirements

## Product goal

TLRPC must make a TL RPC server feel familiar to Go developers who have used
gRPC while remaining TL-native on the wire. Any application must be able to:

1. provide its own valid TL schema;
2. generate typed objects, codecs, and service contracts;
3. implement and register the generated service servers; and
4. serve those services through the reusable Runtime v2 protocol edge.

The schema defines the application API. TLRPC must not define a mandatory
Telegram schema or service set.

## Framework requirements

### Schema and generation

- Accept an application-owned schema as the compatibility source of truth.
- Preserve constructor IDs, TL field shapes, flags, vectors, unions, requests,
  and declared result types.
- Generate deterministic Go code, constructor factories, codecs, service
  interfaces, unimplemented stubs, descriptors, and registration helpers.
- Accept a base schema labeled by `--base-layer`, zero or more repeatable and
  ordered `--layer-diff=<layer>:<path>` inputs, and one target `--layer`.
- Treat delta files as ordinary TL fragments: a same-name declaration replaces
  the prior declaration, a new declaration adds one, and the exact directives
  `// @tlrpc remove constructor NAME` and `// @tlrpc remove function NAME`
  remove declarations from their respective domains.
- Apply layer differences through the target in command-line order and record
  the target layer in generated metadata.
- Resolve all layer differences during generation. Runtime v2 must never
  convert objects, declarations, or method semantics between layers.
- Keep generated packages independent of Telegram product code and `tgserver`.

### Service model

- Generated `Register*Server` helpers and complete generated descriptors are
  the only application registration path.
- Dispatch API calls by their TL method constructor IDs.
- Give handlers typed requests, typed results, `context.Context`, unary
  interceptors, and structured `rpc_error` conversion.
- Keep MTProto wrappers and protocol controls inside Runtime v2 rather than
  generating them as application services.
- Expose immutable request metadata and explicit semantic capabilities; do not
  expose mutable protocol state or a transport connection.

### Runtime v2

- Use one connection runtime for TCP and WebSocket carriers.
- Validate configured and hard frame limits before allocating payloads.
- Bound ciphertext, TL bytes, vectors, containers, and compressed expansion.
- Own the MTProto authorization-key handshake, encryption/decryption, salt and
  session validation, message validation, wrappers, containers, controls,
  acknowledgements, and resend/state behavior.
- Pin each physical connection to one auth key and reject encrypted traffic for
  a different auth key on that connection.
- Host a bounded map of same-auth composite sessions on each physical
  connection, with a default capacity of 16.
- Identify a protocol session by the complete `(AuthKeyID, SessionID)` key.
- Acquire one exclusive active lease per composite key so reconnect cannot
  create two outbound sequence owners.
- Persist detached snapshots through a context-aware `session.Store`.
- Give each composite session independent lease, validator, reliability,
  router, active-request registry, writer, and push-subscription state.
- Use one writer per composite session to own that session's outbound ordering,
  message IDs, sequence numbers, RPC correlation, batching, encryption,
  reliability state, and outbound snapshot progress.
- Serialize complete physical frame writes through one connection-owned sink;
  closing a session writer must not close the physical transport.
- Bound request admission across the physical connection, not independently per
  session.
- Retire only the matching session when its lease is replaced. Retire the
  physical connection when its transport or shared write boundary cannot
  continue safely.
- Propagate disconnect and shutdown cancellation to handlers and writers.

### Application-facing runtime behavior

- Make layer, auth-key ID, client metadata, user ID, and composite binding
  available as immutable context values.
- Apply user bind/unbind requests as explicit mutations after successful
  handler completion and persist them before exposing the new binding.
- Provide `Sender` for semantic schema-defined push from a handler.
- Provide `Server.Publish` for best-effort process-local delivery to active
  sessions bound to an application user.
- Treat `invokeWithoutUpdates` as a composite-session subscription choice:
  requests and responses continue normally while asynchronous pushes are
  suppressed for that session.
- Keep live delivery distinct from durable application update recovery.

### Entry points and operations

- Support MTProto over TCP and WebSocket.
- Keep carrier/framing details behind transport interfaces.
- Bound connection work, handler concurrency, reliability retention, message
  size, deadlines, handshake state, and shutdown lifetime.
- Close listeners and active connections and wait for owned goroutines during
  shutdown.
- Permit future carriers without changing generated service contracts.
- Treat an HTTP/JSON Telegram Bot API as a separate application adapter, not an
  MTProto framing mode.

## Responsibility boundary

TLRPC owns reusable mechanics:

- parsing, validation, generation, and generated registration;
- framing, handshake, cryptography, envelopes, wrappers, controls, reliability,
  and connection lifecycle;
- protocol-session snapshot and lease semantics;
- semantic local sender/publish capabilities and lifecycle observations.

Applications own policy and durable product behavior:

- schema selection and generated package versioning;
- service implementations and authorization rules;
- durable auth-key and session-store adapters and key-at-rest protection;
- users, contacts, dialogs, messages, media, bots, and other domain state;
- recipient selection, transactional outboxes, durable update logs,
  `pts`/`qts`/`seq`, difference APIs, and missed-update recovery;
- databases, queues, object stores, rate policy, and multi-node routing.

## Security and quality requirements

- Never log auth-key material, nonces, or decrypted payloads.
- Bound and expire temporary handshake and reliability state.
- Reject malformed sizes and protocol structures without panic or untrusted
  allocation.
- Persist replay, sequence, salt, layer, client metadata, and binding progress
  as protocol correctness state rather than best-effort telemetry.
- Test protocol behavior with malformed inputs and an independent client.
- Prove arbitrary-schema generation independently of Telegram compatibility.
- Run protocol compatibility over both TCP and WebSocket.
- Keep physical writes behind the connection-owned serialized frame sink.

## Explicit non-goals

- A complete Telegram backend or a fixed Telegram API package.
- Protobuf or gRPC as a public protocol.
- Runtime semantic conversion between historical layers.
- A mutable public session or application-controlled wire state.
- Built-in databases, event buses, distributed presence, or durable fanout.
- Treating process-local push as missed-update recovery.
- Retaining unreleased APIs or the old runtime for backward compatibility.

## Acceptance criteria

Framework acceptance requires a non-Telegram schema to generate and compile,
register multiple typed services, dispatch encrypted requests, and return typed
results and RPC errors through public APIs.

Telegram compatibility is a separate axis. The exact layer-228 fixture must
generate deterministically and Runtime v2 must pass MTProto handshake,
validation, wrapper, control, reconnect, reliability, push, TCP, WebSocket, and
independent-client tests. Passing this axis does not make Telegram TLRPC's
product definition.

`tgserver` adoption is a separate consumer gate. It must preserve its own product
semantics and durable update recovery while replacing its protocol gateway
with released TLRPC APIs.
