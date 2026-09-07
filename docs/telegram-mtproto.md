# Telegram and MTProto

This page documents Telegram-specific protocol behavior implemented by the
generic framework. It does not make Telegram, layer 228, or `tgserver` a TLRPC
dependency and does not replace Telegram's protocol specification.

## TL schemas and layers

TL constructor IDs are wire identities; fields define encoded shape and a
function's declared result defines its generated Go return contract. Runtime v2
keeps three constructor domains separate:

1. built-in TL primitives;
2. MTProto envelopes, wrappers, and controls owned by Runtime v2; and
3. application objects and functions generated from the supplied schema.

Only application functions become generated service methods.

Telegram introduces API changes as layers. A project may resolve an ordered
base/difference history to one selected schema or opt in to one generated
package covering an exact layer set. `invokeWithLayer` records and validates
the client's declared layer, caps it at the generated maximum, and runtime
dispatch uses that effective layer to select generated same-ID request and
nested-constructor layouts.

The multi-layer generator keeps unchanged definitions once, retains base-layer
Go names, and versions changed incoming requests. Same-ID additive objects use
layer-gated superset fields, so shared parents do not need variants solely for
an added flag. A type that is a union in any selected Telegram layer remains
one union containing all historical and current constructor variants; nested
shape propagation stops there. Unknown flag bits are rejected against the
selected known layer. Historical compatibility methods and removed union
constructors remain available when supplied by the generated schema history.
Output objects are recursively cloned by generated projection code; semantic
conversions require an application hook.

The repository's exact Telegram layer-228 schema is a compatibility fixture for
parser/generator determinism and Runtime v2 tests. Custom projects neither
import it nor default to it.

## Wire stack

```text
TCP or WebSocket stream
  -> MTProto framing/obfuscated2
  -> unencrypted auth-key handshake or encrypted envelope
  -> Runtime v2 validation and wrapper/control handling
  -> generated application service
```

TCP supports abridged, intermediate, padded-intermediate, and full framing.
WebSocket is a byte-stream carrier, requires the `binary` subprotocol and
obfuscated2, and applies the origin policy documented in
[implementation.md](./implementation.md#websocket-transport).

## Authorization-key handshake

Runtime v2 owns the permanent authorization-key exchange:

```text
req_pq_multi -> resPQ -> req_DH_params -> server_DH_params_ok
             -> set_client_DH_params -> dh_gen_ok
```

The handshake engine validates nonces and DH parameters, derives the key and
initial salt, and persists the key through `crypto.AuthKeyManager`. Temporary
handshake state is bounded, expiring, and single-use. RSA key files have the
permission protections documented in
[implementation.md](./implementation.md#rsa-server-keys).

An auth key is cryptographic identity; it is not an application user login.

### Unknown authorization keys

When an encrypted frame names an authorization key that
`crypto.AuthKeyManager.Get` reports as `crypto.ErrAuthKeyNotFound`, Runtime v2
writes the signed little-endian transport error `-404` as a four-byte payload
through the connection's negotiated MTProto framing and obfuscation, then
closes the connection. This boundary runs before encrypted-envelope decoding,
session acquisition, or application dispatch and is identical for TCP and
WebSocket carriers.

Only the explicit not-found sentinel maps to `-404`. Other auth-key source
failures, including transient storage errors, remain internal connection
failures and close without a transport error so the client is not told that a
key is absent when its status is unknown.

## Composite sessions and replay

An encrypted message carries auth-key identity plus inner salt, session ID,
message ID, sequence number, and one TL body. The first encrypted auth key pins
the physical connection. The connection may host bounded sessions for that key
but rejects a different key.

Runtime v2 validates each `(AuthKeyID, SessionID)` independently. Message IDs
must have Telegram's client format and remain within the accepted time window.
Content-related and non-content sequence parity is enforced, with bounded
out-of-order content sequence acceptance for restored sessions.

Replay protection combines the highest message ID, exact recent IDs, a durable
message-ID floor, and recent content sequence numbers. When the exact ID window
evicts its smallest value, the floor advances; any future ID at or below the
floor is rejected. Container validation is atomic: the outer message and all
children pass before the snapshot changes.

These guarantees survive reconnect/restart only when the application's durable
`session.Store` preserves every snapshot field. A restored session whose store
has no prior client progress may adopt an already-advanced first odd content
sequence number, after which normal bounded replay rules apply.

Because application handlers and encrypted response retention are process
local, a process can stop after accepting a client RPC but before it produces a
response. On a resend of that exact application message after restart, Runtime
v2 does not invoke the handler twice. It returns a correlated `500
REQUEST_RETRY` and acknowledgement, allowing the client to retry using a new
message ID. This recovery applies only when there is no active handler and no
locally retained response; standalone active and locally completed replays still receive
the canonical bad-message response. A fresh container may include known recent
child retransmissions alongside new requests: those children are separated from
dispatch only after the whole remaining envelope validates. The writer resends
a retained unacknowledged reply, acknowledges an already acknowledged reply or
active request, or returns `REQUEST_RETRY` when process-local reply retention is
missing. No retransmitted child is re-executed. Unknown IDs below the durable
floor, replayed outer containers, and malformed siblings remain rejected.

The snapshot also records whether the session has accepted unsolicited
application pushes. Runtime v2 restores this opt-in and registers the new
connection sender; the sender itself remains process-local. A request wrapped
in `invokeWithoutUpdates` can suppress that request's push intents but cannot
change the durable subscription state.

## Wrappers

Runtime v2 removes protocol wrappers before generated dispatch:

- `invokeWithLayer` records the declared API layer;
- `initConnection` records immutable client metadata;
- `invokeAfterMsg` and `invokeAfterMsgs` wait for earlier successful requests;
- `invokeWithoutUpdates` suppresses asynchronous push for the current session
  while its wrapped RPC and controls continue; and
- `gzip_packed` expands under the shared decode/decompression budget.

`invokeAfter*` must be outermost. Dependency IDs must be earlier than the
current request, are deduplicated, and are capped at 64. The wait is bounded to
500 ms and produces `MSG_WAIT_FAILED` or `MSG_WAIT_TIMEOUT` as described in
[implementation.md](./implementation.md#invokeafter-behavior).

Wrappers count toward one shared logical-request budget; nesting cannot reset
byte, wrapper, object, or gzip limits.

An application may apply the same request-scoped behavior without requiring a
client wrapper by passing explicit normalized method constructor IDs to
`WithNonSubscribingMethods`. This server policy prevents those methods from
creating a cold session push subscription. It does not clear a subscription
already established by another request, and Runtime v2 does not infer file or
transport roles.

## Containers, controls, and writes

Every callable `msg_container` child has its own request ID and receives an
`rpc_result` correlated to that child. Runtime v2 reserves the whole set before
dispatch so overload cannot partially execute a container.

Protocol controls remain independent of application services, including
`msgs_ack`, `msgs_state_req`, `msg_resend_req`, `rpc_drop_answer`,
`get_future_salts`, bad-message/bad-salt responses, and new-session
notification. Reliability records are bounded and expiring and retain exact
encrypted packets where resend requires them.

The per-session writer allocates outbound message IDs/sequence numbers,
correlates results, bounds serialization, encrypts, records reliability state,
and persists progress. A connection-owned bounded sink serializes complete
frames from all of that connection's sessions.

## Telegram application boundary

TLRPC can deliver a generated update object to an active local session with
`Sender` or `Server.Publish`. `Server.PublishProjected` exposes each eligible
session's effective layer and lease generation so the Telegram application can
choose that recipient's representation. TLRPC does not convert Telegram
objects or reuse one layer's bytes for another session. A Telegram server must
still own recipients, users, authorization policy, dialogs, messages, media,
bots, durable update state (`pts`, `qts`, `seq`), outbox/fanout, difference
APIs, and any same-layer projection cache.

`tgserver` demonstrates that boundary: it generates its selected Telegram
schema, implements those services, and supplies durable protocol/product
stores. Its product behavior must not be moved into this generic framework.

Telegram's HTTP/JSON Bot API is a separate application adapter, not another
Runtime v2 MTProto transport.
