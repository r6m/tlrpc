# Requirements

This page defines the v0.12.0 framework contract. Concrete APIs and defaults
are documented in [implementation.md](./implementation.md).

## Product goal

TLRPC must let any Go project define a server API in TL, generate typed service
contracts, implement those contracts, and serve them through a reusable
TL/MTProto runtime. The programming model may feel familiar to gRPC users, but
protobuf and gRPC are not part of the architecture.

The framework must remain schema-neutral. Telegram schemas and `tgserver` are
compatibility consumers, not framework dependencies.

## Schema and generation

- The application-owned TL schema is the API and compatibility source of truth.
- Generation must preserve constructor IDs, field shapes, flags, vectors,
  unions, functions, and declared result types.
- Generated output must include typed objects and codecs, service interfaces,
  unimplemented stubs, static descriptors, and `Register*Server` helpers.
- Output and provenance must be deterministic for the same selected schema.
- Optional layer differences must resolve only during generation from one
  labeled base, ordered differences, and one selected target.
- A same-name declaration replaces the earlier declaration, a new name adds
  one, and exact `@tlrpc remove` directives remove declarations.
- Runtime v2 must not translate API layers, constructor IDs, object shapes, or
  method semantics. One generated package represents one resolved layer.

## Service model

- Generated registration is the only application method-registration path.
- Dispatch must use TL method constructor IDs and typed request/result shapes.
- Handlers receive `context.Context`, immutable request metadata, optional
  unary interception, and explicit semantic capabilities.
- Handlers must not receive raw transports or mutable protocol-session state.
- Runtime-owned wrappers, containers, controls, acknowledgements, and errors
  must not become generated application services.
- Panics and unknown internal errors must cross the wire only as sanitized
  internal RPC errors; intentional structured RPC errors must remain intact.

## Runtime v2

- TCP and WebSocket carriers must enter the same Runtime v2 connection path.
- Each physical connection must pin to one auth key and may host only a bounded
  number of sessions for that same key.
- A protocol session is identified by `(AuthKeyID, SessionID)` and must have one
  active `session.Lease` from a `session.Coordinator` so reconnect cannot create
  concurrent outbound sequence owners.
- Every successful lease acquisition must carry a monotonically increasing
  generation for that session key, cancel the replaced owner, and fence Save and
  Delete so stale generations cannot mutate durable protocol state.
- After ownership loss, the old physical connection must reject that session
  key permanently while preserving unrelated multiplexed sessions; it must not
  reacquire a later generation and ping-pong ownership.
- Each session owns independent validation, replay state, reliability, routing,
  active requests, writer ordering, and live-push subscription state.
- One connection-owned sink must serialize complete physical writes across its
  sessions; a session writer must not directly own or close the transport.
- Request admission must be atomic for a whole container and bounded across the
  physical connection. Rejected work must not consume durable protocol state.
- Shutdown and disconnect cancellation must reach handlers and writers.

## Replay and ordering

- Validation must atomically accept an outer envelope and all container
  children or reject the whole input without committing a partial snapshot.
- Duplicate client message IDs must remain rejected after the bounded recent-ID
  window evicts entries. The monotonic client message-ID floor is therefore a
  required durable snapshot field.
- Recent client message IDs and recent content sequence numbers must also be
  detached, persisted protocol state.
- A durable `session.Coordinator` implementation must round-trip every
  `session.Snapshot` field through its Save/Delete fencing path, including
  `ClientMsgIDFloor`, `RecentClientMsgIDs`, `RecentClientSeqNos`, and the
  durable push subscription opt-in. A reconnect must restore that opt-in to the
  new process-local sender; `invokeWithoutUpdates` remains request-scoped and
  must not opt a cold session in.
- `invokeAfterMsg` and `invokeAfterMsgs` must wait for successful completion of
  their referenced earlier requests, fail after a failed dependency, and time
  out rather than wait indefinitely for an unknown dependency.

## Resource and security boundaries

- Declared frame sizes must be checked before payload allocation.
- One logical inbound request must share aggregate limits across nested
  wrappers, containers, vectors, generated object nodes/depth, decoded bytes,
  and gzip expansion/work. Nested decoding must not receive fresh budgets.
- Application responses must be bounded while being serialized, before
  encryption or physical write queueing.
- Physical write queues and end-to-end write deadlines must be bounded.
- Connection admission must support global, remote-IP, and auth-key quotas;
  sessions per connection and active handlers must also be bounded.
- WebSocket upgrades must require GET and the `binary` subprotocol, enforce an
  explicit origin policy, and bound upgrade admission and HTTP headers/timeouts.
- RSA private-key loaders must reject non-regular or group/world-accessible
  files. Framework-written private keys must use mode `0600`.

## Observability

- Observation must not execute application callbacks on the protocol path.
- Events must be typed and cover connection, handshake, session, RPC,
  admission, physical writer, session store, and gauges.
- Event fields and error classifications must not expose auth-key material,
  plaintext payloads, private keys, or raw internal error strings.
- Slow or panicking observers must not block or crash Runtime v2. Applications
  must tolerate dropped events and derive authoritative business state from
  their own durable stores.

## Application responsibilities

TLRPC intentionally does not provide:

- product users, authentication policy, dialogs, messages, media, or bots;
- durable application update logs, difference APIs, recipient selection, or
  cross-process fanout;
- databases, queues, object stores, migrations, or retention policy;
- an HTTP/JSON Bot API or arbitrary HTTP RPC gateway; or
- deployment topology and tenant-specific limits.

`Sender` and `Server.Publish` are process-local live-delivery tools. An
application must commit durable product state before treating live delivery as
an optimization.

## v0.12.0 acceptance

The release requires deterministic custom-schema and Telegram layer-228
generation, focused malformed-input and resource-limit tests, Runtime v2 tests,
TCP and WebSocket conformance, replay/reconnect evidence, race checks, vet,
builds, and architecture guards with sequential Go package compilation.
