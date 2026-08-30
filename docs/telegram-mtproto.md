# Telegram and MTProto

This document explains the Telegram-specific compatibility target carried by
TLRPC's general Runtime v2. It is implementation guidance, not a definition of
TLRPC's product scope and not a replacement for Telegram's protocol
specification.

## TL schemas and layers

TL describes constructor-tagged objects and RPC functions:

```tl
user#8f97c628 id:long first_name:string = User;

---functions---
users.getUsers#0d91a548 id:Vector<InputUser> = Vector<User>;
```

The constructor ID is the wire identity. Fields define the encoded shape; the
declared result determines the generated Go return contract. Flags represent
optional fields, vectors represent repeated values, and a result with multiple
constructors becomes a generated union interface.

TLRPC separates three domains:

1. built-in TL primitives;
2. Runtime v2 MTProto envelopes, wrappers, and control objects; and
3. application objects and functions generated from the supplied schema.

Only functions in the application schema become application service methods.

A Telegram layer is a generation concern. A new layer may add or remove
declarations or change fields and constructor IDs. Start from a base schema and
its `--base-layer`, provide repeatable ordered
`--layer-diff=<layer>:<path>` fragments, and select the generated target with
`--layer`.

Each fragment uses ordinary TL declarations. A same-name constructor or
function replaces the previous declaration in its domain, while a new name
adds a declaration. Removal is explicit and uses these exact comment
directives:

```tl
// @tlrpc remove constructor NAME
// @tlrpc remove function NAME
```

The generator applies the fragments in command-line order for the selected
target, validates the resulting schema, and records that target in generated
descriptors. `invokeWithLayer` records and validates the client's declared
layer at the protocol edge; it does not select a different generated schema or
convert old objects, fields, constructor IDs, or method semantics at runtime.

The repository's exact Telegram layer-228 schema is a compatibility fixture.
It proves deterministic parsing/generation and Runtime v2 behavior against a
large real schema. Layer 228 is not selected automatically and is not imported
by a custom generated package. Telegram consumers may generate layer 228 from
a reviewed base plus ordered delta fragments or supply an already resolved
layer-228 schema snapshot.

## Wire stack

```text
TCP or WebSocket byte stream
  -> MTProto framing
  -> obfuscated2 where configured/required
  -> unencrypted handshake OR encrypted MTProto envelope
  -> Runtime v2 validation and routing
  -> protocol control OR generated application method
```

TCP supports abridged, intermediate, padded-intermediate, and full framing.
WebSocket is a byte-stream carrier rather than a second packet protocol; its
frame boundaries do not define MTProto packet boundaries. TLRPC's WebSocket
entry point requires the `binary` subprotocol and obfuscated2.

Every carrier uses the same bounded `ReadMessage(maxPayloadBytes)` contract.
Declared frame sizes are rejected before allocation when they exceed the
configured or hard ceiling. Runtime v2 additionally bounds inner encrypted
lengths, TL byte strings, vectors, containers, and `gzip_packed` expansion.

## Authorization-key handshake

Before encrypted traffic, the client and server establish a permanent auth key:

```text
req_pq_multi
  -> resPQ
  -> req_DH_params
  -> server_DH_params_ok
  -> set_client_DH_params
  -> dh_gen_ok
```

The server-owned handshake engine selects an RSA key, validates nonces and DH
parameters, derives the auth key and initial server salt, and persists the key
through `crypto.AuthKeyManager`. Temporary handshake state is capacity-bounded,
expires, is scoped to the server/connection, and is consumed once.

An auth key is cryptographic identity. It is neither an application login nor
an MTProto session.

## Encrypted messages and composite sessions

An encrypted envelope identifies an auth key and carries inner data containing:

- server salt;
- session ID;
- message ID;
- sequence number; and
- one TL body.

Runtime v2 resolves the auth key, decrypts and validates the envelope, and
acquires the session identified by `(AuthKeyID, SessionID)`. Message IDs are
directional and time-related. Sequence numbers distinguish content-related
messages; client and server sequence progress are independent.

The first encrypted auth key pins the physical connection. That connection may
host a bounded map of composite sessions for the same auth key—16 by default—
but encrypted traffic for another auth key is rejected.

One exclusive lease owns a composite session at a time. On reconnect, the old
owner of that matching session is retired before the replacement can allocate
server IDs or sequence numbers. Other sessions on the physical connection are
unaffected. Durable state crosses `session.Store` as detached snapshots,
including salt, layer, client metadata, binding, replay history, and directional
progress.

Application authentication may bind a user ID to the protocol session through
`BindSessionUser`. This opaque binding does not replace either auth-key or
session identity.

## Wrappers

Runtime v2 normalizes Telegram MTProto wrappers before application dispatch:

- `invokeWithLayer` records/validates the declared layer and invokes its query;
- `initConnection` records immutable client metadata and invokes its query;
- `invokeAfterMsg` and `invokeAfterMsgs` express ordering dependencies;
- `invokeWithoutUpdates` invokes its query and disables asynchronous push for
  that composite session;
- `gzip_packed` expands a bounded compressed body and resumes decoding.

Wrappers can be nested only within configured/hard depth and decode budgets.
They are runtime behavior, not services that an application implements.

`invokeWithoutUpdates` is composite-session-local subscription state for the
wrapped invocation. It does not disable RPC results, acknowledgements, or
protocol controls, and it does not alter another session on the same physical
connection or sharing the auth key or user binding.

## Containers, correlation, and outbound ordering

`msg_container` carries multiple inner messages. Each callable child has its
own message ID and receives an `rpc_result` correlated to that child ID; the
outer container ID is not used as a synthetic request ID.

Application handlers and protocol controls return outbound intents. The
per-session writer alone allocates that session's server message IDs and
sequence numbers, wraps correlated results, constructs intentional containers,
encrypts, records reliability state, and persists outbound progress. A
concurrent push therefore enters the same session ordering boundary as RPC and
control traffic.

Every session writer submits complete encrypted frames to one connection-owned
sink. The sink serializes physical writes across the connection but does not
share protocol ordering or sequence state between sessions. Closing one session
writer never closes the transport; physical transport closure belongs to
connection shutdown. Request admission is bounded connection-wide.

## Protocol controls and reliability

Runtime v2 handles protocol controls independently of application handlers,
including:

- `msgs_ack` acknowledgement consumption;
- `msgs_state_req` canonical state reporting;
- `msg_resend_req` exact resend when retained, with state fallback;
- `rpc_drop_answer` active request cancellation and unknown-target response;
- `get_future_salts` protocol salt responses;
- bad-message and bad-server-salt responses;
- new-session notification.

Reliability records are bounded and expiring. Exact encrypted packets are
retained when required for protocol resend. Protocol reliability is distinct
from Telegram application update recovery.

## Server push and Telegram updates

Telegram update delivery has two layers:

- live delivery to currently connected sessions; and
- durable recovery through application state such as `pts`, `qts`, `seq`,
  `updates.getState`, and `updates.getDifference`.

TLRPC supplies only the generic live-delivery edge. `Sender.Send` targets the
current subscribed composite session; `Server.Publish` targets locally active
subscribed sessions bound to an opaque user ID. Both submit schema-defined
objects to each target session's writer.

A Telegram application such as `tgserver` must choose recipients, commit the
durable update log/outbox first, implement difference semantics, and treat
live delivery as an optimization. TLRPC does not provide cross-process fanout
or product update state.

## Typical Telegram client flow

```text
connect TCP/WebSocket
  -> negotiate framing/obfuscation
  -> create or reuse auth key
  -> send invokeWithLayer(initConnection(query))
  -> call configuration methods
  -> perform application authentication
  -> bind the application user to the protocol session
  -> call generated application services
  -> receive correlated results and subscribed live updates
  -> recover missed updates through application difference methods
```

The compatibility harness and exact layer-228 fixture validate protocol
behavior and selected startup flows. They do not implement or prove a complete
Telegram product backend.

## `tgserver` boundary

`tgserver` generates contracts from its selected Telegram schema, implements
those generated services, provides durable auth-key and composite-session
stores, and uses TLRPC for TCP/WebSocket protocol handling.

Authentication policy, users, dialogs, messages, media, bots, durable updates,
and deployment topology remain in `tgserver`. Reusable MTProto mechanics may
be validated against its existing gateway, but product semantics must not move
into TLRPC.

## Bot API boundary

Telegram's Bot API is HTTP/JSON with its own routes and semantics. It may call
the same application services, but it is a separate adapter—not an MTProto
transport or Runtime v2 framing mode.
