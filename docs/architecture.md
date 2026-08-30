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
will sit above this boundary as a consumer.

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
internal/runtime    Runtime v2 leases, routing, validation, reliability, writer
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
  -> acquire composite session lease
  -> inbound validation and snapshot transition
  -> wrapper/container normalization
  -> protocol-control router OR generated application dispatcher
  -> explicit outbound intents and session mutations
  -> per-connection single writer
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

One writer per connection exclusively owns:

- outbound ordering;
- server message-ID and sequence-number allocation;
- `rpc_result` correlation;
- container construction and batching;
- encryption and physical transport writes;
- exact sent-packet retention and acknowledgement/resend state;
- persistence of outbound session progress.

The commit order prevents another component from observing or reusing
partially committed protocol progress. A fatal or ambiguous outcome retires
the connection and its lease.

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
previous owner before the replacement may allocate outbound IDs or sequence
numbers. Runtime code mutates its lease-local state; persistence crosses the
application boundary only as detached `session.Snapshot` values through
`session.Store`.

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
and resend. Reconnect may recover retained protocol reliability state without
creating a second writer owner.

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
numbers, encryption, or batching. Its push enters the same single writer as
RPC and control output.

## Push subscriptions and publish

`invokeWithoutUpdates` changes only the current connection's asynchronous-push
subscription. The wrapped request still executes and receives its correlated
result, while handler sends and user-targeted local publish are suppressed for
that connection. Other sessions for the same auth key or user are unaffected.

`Server.Publish(userID, object)` sends a schema-defined object to locally
active subscribed sessions bound to that user. It is process-local and
best-effort. Durable update history, recipient choice, transactional outbox,
and missed-update recovery remain application responsibilities.

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
state, or perform a physical write outside the single writer.

## Framework/application split

TLRPC owns wire correctness and reusable protocol mechanics. A consuming
application owns all product behavior, durable domain state, authorization
policy, and distributed infrastructure. In particular, Telegram concepts such
as dialogs, media, login workflows, `pts`, `qts`, update difference, and bot
HTTP routes are not Runtime v2 concerns.
