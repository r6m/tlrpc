# Architecture

## System boundary

TLRPC has two cooperating halves:

```text
application-owned TL schema
  -> parser and generator
  -> generated objects, codecs, services, and descriptors
  -> application implementations registered with Server

TCP/WebSocket client
  -> bounded carrier framing
  -> Runtime v2
  -> generated method dispatcher
  -> application implementation
```

“gRPC-like” describes the generated Go programming model: typed server
interfaces, registration helpers, unimplemented stubs, contexts, interceptors,
and structured errors. The public wire remains TL/MTProto.

The framework is schema-neutral. Telegram layer 228 exercises the same parser,
generator, descriptor, and runtime paths as an arbitrary schema. `tgserver`
sits above this boundary as a consumer.

## Component ownership

```text
cmd/tlrpc-gen       schema-to-Go command
internal/parser     TL lexer, parser, AST, and validation
internal/generator  schema-derived types, codecs, and service generation
transport           bounded TCP/WS carriers, framing, and obfuscated2
internal/handshake  server-owned MTProto authorization-key handshake
mtproto             envelope, crypto-adjacent, and protocol helpers
mtproto/tl          runtime-owned wrappers and control objects
session             detached snapshots and persistence contract
internal/runtime    Runtime v2 connections, sessions, writers, and frame sink
root tlrpc package  Server, service registration, context, binding, and push API
```

Generated application packages depend on the public root contracts. Internal
runtime packages do not depend on a Telegram-generated package or `tgserver`.

## Generation-time layer resolution

Layer resolution belongs entirely to the parser/generator half of the system.
The generator reads one base schema, associates it with `--base-layer`, and
applies repeatable `--layer-diff=<layer>:<path>` fragments in command-line
order until it resolves the requested `--layer`.

A difference file uses ordinary TL declarations. Within the constructor and
function domains independently, a declaration with an existing name replaces
that declaration and a declaration with a new name adds it. These exact comment
directives remove declarations:

```tl
// @tlrpc remove constructor NAME
// @tlrpc remove function NAME
```

The resolved schema is then validated and generated exactly like a standalone
schema snapshot. Generated descriptors record the target layer. Runtime v2
receives only the resolved generated contracts; it does not load difference
files or translate requests, responses, fields, constructor IDs, or method
semantics between layers.

## Runtime v2 connection flow

```text
bounded frame
  -> handshake or encrypted envelope decode
  -> auth-key lookup
  -> pin physical connection to one auth key
  -> select or acquire same-auth composite session
  -> per-session validation and snapshot transition
  -> per-session wrapper/container normalization and routing
  -> explicit outbound intents and session mutations
  -> per-session writer
  -> connection-owned serialized frame sink
  -> encoded/encrypted frame
```

There is one production connection path. The old mixed runtime is not retained
as a fallback, flag, or compatibility mode.

### Inbound ownership

The carrier validates lengths before allocation and yields complete bounded
payloads. Runtime v2 owns envelope decoding, decryption, auth-key/session
identity, salt, message-ID and sequence validation, replay protection, nested
decode budgets, wrappers, containers, and control objects.

After normalization, protocol controls remain inside the runtime. Only a
schema method with a complete generated descriptor reaches an application
handler. The handler receives immutable metadata and semantic operations, not
the raw transport or mutable wire state.

### Outbound ownership

Dispatchers produce semantic intents: an RPC result correlated to an inbound
message, a protocol response, an acknowledgement, a resend/state response, a
server push, or an intentional batch. They never write a socket.

Each composite session has one writer that exclusively owns that session's:

- outbound ordering;
- server message-ID and sequence-number allocation;
- `rpc_result` correlation;
- container construction and batching;
- encryption;
- exact sent-packet retention and acknowledgement/resend state;
- persistence of outbound session progress.

All session writers submit complete encrypted frames to one connection-owned
sink, which serializes physical transport writes. The sink's `Close` is a no-op
for session writers: only connection shutdown closes the transport. This
physical serialization does not merge protocol ordering, message IDs, sequence
numbers, reliability, or persistence across sessions.

Request admission is bounded across the physical connection. The per-session
commit order prevents another component from observing or reusing partially
committed protocol progress. A fatal session outcome retires that session;
failure of the shared transport or frame sink retires the connection.

## State model

### Auth keys

Long-lived MTProto auth-key material is stored through
`crypto.AuthKeyManager`. RSA server keys are supplied through
`crypto.ServerKeyManager`. Applications choose durable storage and encryption
at rest; TLRPC owns protocol use of the keys.

### Composite protocol sessions

A protocol session is identified by:

```text
(AuthKeyID, SessionID)
```

Runtime v2 acquires one exclusive lease for that key. A reconnect retires the
previous owner of that matching composite key before the replacement may
allocate outbound IDs or sequence numbers. Other sessions on the old owner's
physical connection are not retired. Runtime code mutates its lease-local
state; persistence crosses the application boundary only as detached
`session.Snapshot` values through `session.Store`.

A physical connection is pinned to the first encrypted auth key it accepts. It
may host a bounded map of composite sessions for that same auth key—16 by
default—but never a session for another auth key. Each mapped session owns its
lease, validator, reliability state, router, active-request registry, writer,
and push subscription independently.

Snapshots contain named protocol state: identity, server salt, negotiated
layer, immutable client metadata, user binding, independent inbound/outbound
sequence progress, replay-window message IDs, new-session markers, and
timestamps. They do not contain generic application data.

There is no public mutable session and no pointer-retaining manager. Business
sessions and product data belong in application storage keyed by identities
the application chooses.

### Handshake state

Each server owns a bounded, expiring handshake engine. A connection-scoped
handshake consumes temporary nonce/DH state once and returns an explicit auth
key and initial salt to Runtime v2. Temporary handshake state is neither global
application state nor a protocol session.

### Reliability state

Runtime v2 tracks bounded, expiring sent-message records by composite session.
The writer records exact encrypted output for acknowledgement, state queries,
and resend for its session. Reconnect may recover retained protocol reliability
state without creating a second writer owner for that composite key.

## Generated dispatch

Each generated service package contains complete static `ServiceDesc` and
`MethodDesc` values. A generated `Register*Server` helper registers an
application implementation through `Server.RegisterService`. Descriptors carry
the request constructor ID, request factory, handler, service/method identity,
and schema layer.

There is no descriptor inference, global constructor registry, or direct raw
method/constructor registration API. Built-in wrappers and controls are
registered and routed internally by Runtime v2.

## Handler capabilities

Handler context exposes immutable values for negotiated layer, auth-key ID,
client metadata, user ID, and the current composite `Binding`.

`BindSessionUser` and `UnbindSessionUser` collect explicit mutations. Runtime
v2 commits them to the snapshot after successful handler completion before
publishing binding presence. The user ID is opaque application identity.

`SenderFromContext` returns a semantic `Sender` that accepts a schema-defined
TL object. `Sender` does not reveal the transport, message IDs, sequence
numbers, encryption, or batching. Its push enters the current session's writer,
the same ordering boundary as that session's RPC and control output.

## Push subscriptions and publish

`invokeWithoutUpdates` changes only the current composite session's
asynchronous-push subscription. The wrapped request still executes and receives
its correlated result, while handler sends and user-targeted local publish are
suppressed for that session. Other sessions on the same physical connection or
for the same auth key or user are unaffected.

`Server.Publish(userID, object)` sends a schema-defined object to locally
active subscribed sessions bound to that user. It is process-local and
best-effort. Durable update history, recipient choice, transactional outbox,
and missed-update recovery remain application responsibilities.

`Server.PublishExcept(userID, binding, object)` has the same delivery behavior
but skips the one active session whose `(AuthKeyID, SessionID)` equals the
supplied immutable binding. This supports application workflows that publish a
committed update to a user's other devices without echoing it to the request's
own protocol session.

## Extension points

The final architecture intentionally keeps a small set of extensions:

- generated service implementations and unary interceptors;
- `session.Store`;
- `crypto.AuthKeyManager` and `crypto.ServerKeyManager`;
- bind/unbind lifecycle hooks carrying immutable `Binding` and semantic
  `Sender` where appropriate;
- transport listener/connection interfaces for compatible carriers;
- logger, limits, deadlines, and reliability configuration.

None of these extensions may bypass generated dispatch, expose mutable runtime
state, or perform a physical write outside the connection-owned serialized
frame sink.

## Framework/application split

TLRPC owns wire correctness and reusable protocol mechanics. A consuming
application owns all product behavior, durable domain state, authorization
policy, and distributed infrastructure. In particular, Telegram concepts such
as dialogs, media, login workflows, `pts`, `qts`, update difference, and bot
HTTP routes are not Runtime v2 concerns.
