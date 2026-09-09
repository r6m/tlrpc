# Implementation

This page documents the current v0.12.0 code surface. Requirements are in
[requirements.md](./requirements.md); ownership and flow are in
[architecture.md](./architecture.md).

## Generator

Generate a package from any application-owned schema:

```bash
tlrpc-gen --schema=./schema.tl --out=./gen --package=gen
```

Generated files contain concrete and union types, TL codecs, constructor
factories, request wrappers, service interfaces, unimplemented service stubs,
static descriptors, registration helpers, `SchemaLayer`, and provenance with
the schema digest.

Layer-difference mode accepts one base, strictly increasing differences, and a
target equal to either the base layer or one supplied difference:

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

Differences are normal TL fragments. Same-name constructors/functions replace
earlier declarations and new names append. Removals use exact directives:

```tl
// @tlrpc remove constructor old.constructor
// @tlrpc remove function old.function
```

Differences after the selected target are not applied. Generation validates
duplicate domains and removal targets. Runtime dispatch remains constructor
driven and does not select layers.

Multi-layer mode emits one package for an exact base/difference history:

```bash
tlrpc-gen \
  --schema=./schema/base.tl \
  --base-layer=228 \
  --layers=228,229 \
  --layer-diff=229:./schema/layers/229.tl \
  --out=./gen \
  --package=gen
```

The generated package preserves unsuffixed base names and emits suffixes only
for changed requests or incompatible concrete shapes. Response-only changes do
not create service methods. Same-ID request variants receive inclusive
`MinLayer`/`MaxLayer` descriptor ranges; zero means unbounded. A sole historical
form may remain unbounded, while same-ID forms must have disjoint ranges.

A type that is a union in any selected snapshot remains one unsuffixed union
for the complete history. Changed same-name constructors receive their own
`Layer<N>` concrete variants inside that union, and removed constructors remain
available for historical decoding. References to the union stay unsuffixed.
Incompatible single-constructor types may propagate a required static parent
shape, but propagation stops when it reaches a stable union owner.

Additive flagged objects use one superset struct. Their codecs obtain the wire
layer from `mtproto.TLLayer`; layer zero means the base layer. Decoding rejects
flag bits unknown to the selected layer, and encoding omits fields unavailable
at that layer. Layered nested decoding uses `NewConstructorForLayer`, and
same-ID request decoding uses `NewMethodRequestForLayer`.

The base schema may contain multiple accepted constructor IDs for one method.
It must contain exactly one unannotated canonical declaration and place
`// @tlrpc variant-layer <N>` immediately before each historical declaration.
IDs and positive variant layers must be unique, and a variant layer cannot
exceed the declared base layer. The annotation controls generated name
provenance, not session acceptance: every form included in the baseline remains
available from the generated package's base layer upward. The base is the
package's supported floor, not a universal TL protocol minimum. Generated names
retain provenance, for example `AuthSignUpRequestLayer172` and
`UpdatesGetDifferenceRequestLayer158`.

Layered output also provides `ProjectTLObject(source, targetLayer, hook)`. It
recursively clones response objects, vectors, and unions into the target wire
shape without reflection. Automatic projection rejects semantic or lossy
changes unless the application hook supplies an explicit replacement. A hook
replacement marked handled re-enters automatic projection with the replacement
root hook skipped. Nested children still invoke hooks and undergo normal layer
validation and cloning. Request objects are rejected by this output-only API.

## Registration and handlers

```go
server := tlrpc.NewServer(options...)
gen.RegisterEchoServer(server, echoService)
err := server.Serve(listener)
```

Generated registration installs complete `ServiceDesc`/`MethodDesc` metadata.
For layered packages, `ServiceDesc.SchemaLayer` is the highest supported layer
and each changed request variant carries its accepted range. Runtime v2 selects
among same-ID request descriptors using the effective request layer, decodes
the generated typed request, runs unary interceptors, invokes the typed method,
and encodes its declared TL result. Generated unimplemented stubs return
structured unimplemented errors.

Request context exposes immutable values such as layer, auth-key ID, client
metadata, user binding, and semantic sender. `BindSessionUser` and
`UnbindSessionUser` request named post-handler mutations; successful mutations
are persisted by Runtime v2. No handler API exposes the raw connection.

Panics are always recovered at the application boundary. Unknown errors become
sanitized `500 INTERNAL` responses; explicit `RPCError` values, including
wrapped values, preserve their intentional code and message.

## Session storage

`session.Coordinator` is the public Runtime v2 ownership contract. It acquires
one active `session.Lease` for a `session.SessionKey{AuthKeyID, SessionID}` and
returns a monotonically increasing generation for every successful takeover.
The lease context is canceled when ownership is lost, released, or retired by
Runtime v2 after a fatal session error.

Runtime v2 saves and deletes snapshots only through the active lease:

```go
type Coordinator interface {
	Acquire(ctx context.Context, key SessionKey, initial Snapshot) (Lease, error)
}

type Lease interface {
	Key() SessionKey
	Generation() int64
	Context() context.Context
	Done() <-chan struct{}
	Created() bool
	Snapshot() (Snapshot, error)
	Save(ctx context.Context, next Snapshot) error
	Delete(ctx context.Context) error
	Retire(cause error)
	Release()
}
```

`session.NewLocalCoordinator(store)` is the default in-process implementation.
It cancels the replaced owner, waits for release before loading replacement
state, assigns increasing generations, and rejects stale Save/Delete attempts.
It is suitable for tests and single-process development. Multi-process
deployments should provide their own Coordinator backed by a durable fencing
primitive.

When a lease ends with `session.ErrLeaseLost`, Runtime v2 poisons that composite
session key on the old physical connection. The connection may continue serving
other sessions for the pinned auth key, but it cannot acquire a newer lease for
the replaced key. When the replaced key was its final live session, Runtime v2
closes the displaced physical connection so the client observes ownership loss.
Poisoned keys are bounded by the configured connection-session capacity;
reaching that bound also retires the physical connection.

`session.Store` remains the detached snapshot storage primitive used by the
local coordinator. Stores load, create, save, and delete copies keyed by
`session.SessionKey{AuthKeyID, SessionID}`. Durable implementations must
preserve all fields, including:

- salt, layer, user binding, client metadata, and timestamps;
- independent client/server sequence progress and session notification state;
- highest/first client message IDs;
- `ClientMsgIDFloor` and `RecentClientMsgIDs`; and
- `RecentClientSeqNos`; and
- the push-subscription opt-in, which restores sender registration after a
  reconnect while keeping `invokeWithoutUpdates` request-scoped.

Slices must be copied on store boundaries. Configure an external coordinator
with `tlrpc.WithSessionCoordinator`. Configure only detached local storage with
`tlrpc.WithSessionStore`, which is wrapped by the default local coordinator.
The included memory store is useful for tests and single-process development;
it does not make replay state durable across process restart.

## Resource limits

`NewServer` has bounded defaults:

| Boundary | Default |
| --- | ---: |
| MTProto payload | 16 MiB |
| in-flight application requests | 1024 |
| connections | 4096 |
| connections per remote IP | 64 |
| connections per auth key | 4 |
| sessions per physical connection | 16 |
| decoded bytes per logical request | 16 MiB |
| wrappers | 16 |
| containers | 64 |
| aggregate vector elements | 65,536 |
| generated object nodes | 262,144 |
| generated object depth | 128 |
| gzip expansion ratio | 128x |
| gzip work | 32 MiB |
| encoded TL response | 16 MiB |
| recovery push barrier queue | 64 objects / 16 MiB |
| read timeout | 2 minutes |
| write timeout | 30 seconds |

The physical write queue also has a bounded Runtime v2 default. Configure the
policy as one TL-native unit:

```go
tlrpc.WithResourceLimits(tlrpc.ResourceLimits{
	MaxPayloadBytes:          16 << 20,
	MaxInFlightRequests:      1024,
	MaxConnections:           4096,
	MaxConnectionsPerIP:      64,
	MaxConnectionsPerAuthKey: 4,
	MaxSessionsPerConnection: 16,
	ReadTimeout:              2 * time.Minute,
	WriteTimeout:             30 * time.Second,
	ShutdownGracePeriod:      15 * time.Second,
})
```

When `WithResourceLimits` is supplied, payload, in-flight request, read
timeout, and write timeout values must be positive. Negative values are invalid
for every optional dimension. Zero selects safe defaults for decode,
decompression, response encoding, and physical write queue dimensions; zero
means unlimited for connection/IP/auth-key quotas; zero session capacity keeps
the default; and zero shutdown grace means immediate forced shutdown after
listener cancellation.

One `mtproto.DecodeBudget` is shared through the complete logical inbound
decode. Generated `DeserializeTL` methods charge object nodes/depth, vector
readers charge aggregate elements, wrapper/container parsers charge their
counts, and `gzip_packed` charges decoded bytes, expansion ratio, and work.
Response serialization writes through a bounded writer before encryption.

When `WithRecoveryPushBarrier` is enabled, its per-session FIFO reuses
`PhysicalWriteQueueCapacity` as its object-count limit and
`MaxEncodedResponseBytes` as its aggregate byte limit. Exceeding either limit
returns `ErrRecoveryPushBarrierFull` to the publisher and retires that exact
session so its client must recover from durable application state.

Configure the opt-in with application method constructor IDs:

```go
server := tlrpc.NewServer(
	tlrpc.WithRecoveryPushBarrier(getDifferenceID, getChannelDifferenceID),
)
```

The gate starts immediately before generated application dispatch and ends
only after the correlated `rpc_result` or `rpc_error` is written. A push to
that exact session returns successfully once Runtime v2 copies it into the
bounded FIFO; success does not mean physical delivery. The FIFO drains in
order after the reply. Other target sessions in the same publish fanout
continue independently.

If cancellation or a write failure prevents the protected reply from reaching
the wire, Runtime v2 discards the queued FIFO and retires that session. The
replacement session must recover from durable application state; queued bodies
from the retired lease generation are never replayed.

The barrier is scoped to one `(AuthKeyID, SessionID)` generation. Other
sessions under the same authorization key, and other authorizations bound to
the same user, are outside that request's barrier.

Applications can prevent selected methods from making cold sessions eligible
for live push:

```go
server := tlrpc.NewServer(
	tlrpc.WithNonSubscribingMethods(uploadGetFileID, uploadSaveFilePartID),
)
```

Runtime v2 matches the normalized inner application constructor, including a
method nested in `invokeWithLayer` or `initConnection`. A matching request uses
the same request-scoped suppression as `invokeWithoutUpdates`: it does not add
a push subscription, its `Sender` drops request-scoped sends, and push intents
in its outcome are removed. The option is empty by default and does not clear
an existing durable subscription. Applications must provide explicit schema
constructor IDs; Runtime v2 does not infer connection or method roles.

`WithReliabilityLimits` separately controls the bounded session/message
retention and TTL used by MTProto ACK/state/resend behavior.

## `invokeAfter` behavior

Runtime v2 supports `invokeAfterMsg` and `invokeAfterMsgs` as outermost wrappers
with at most 64 valid earlier dependency IDs. Completion outcomes are retained
per session in a history bounded by active-request capacity.

- all dependencies successful: dispatch the wrapped method;
- any dependency failed or was canceled: return `500 MSG_WAIT_FAILED`;
- unknown dependency still unresolved after 500 ms: return
  `500 MSG_WAIT_TIMEOUT`; and
- dependent request canceled: stop waiting and do not dispatch.

Application success means Runtime v2 produced a correlated `RPCResult` and no
correlated `RPCError` for the request.

## Observer

Configure typed observation with `tlrpc.WithObserver(observer)`. An observer
implements:

```go
type Observer interface {
	ObserveTLRPC(tlrpc.Event)
}
```

The event variants are `ConnectionEvent`, `HandshakeEvent`, `SessionEvent`,
`RPCEvent`, `AdmissionEvent`, `WriterEvent`, `StoreEvent`, and `GaugeEvent`.
They report stable identifiers, classifications, durations, counts, and error
codes rather than payloads or secret material.

Delivery is asynchronous through an internal 256-event channel. Emission is
non-blocking: a full channel drops the new event. Observer callback panics are
recovered. Observers should return quickly and export metrics/logs elsewhere;
they must eventually return and must not be used to make protocol correctness
decisions. A callback that never returns violates the observer contract; it
cannot block Runtime v2 or server shutdown, but its observer worker cannot be
reclaimed by Go while user code remains blocked.

## WebSocket transport

`transport.WebSocketTransport` requires:

- an HTTP `GET` upgrade;
- `Sec-WebSocket-Protocol: binary`;
- obfuscated2 over the WebSocket byte stream; and
- bounded upgrade admission, header size, header-read timeout, and idle timeout.

`WebSocketOriginPolicy` controls browser origins:

```go
transport.WebSocketOriginPolicy{
	AllowedOrigins: []string{"https://app.example"},
	AllowMissing:   false,
}
```

Origins are canonicalized to lower-case scheme and host. With a non-empty
allowlist, only listed origins are accepted and a missing `Origin` is accepted
only when `AllowMissing` is true. With an empty policy, browser origins must be
same-origin and missing origins are accepted for non-browser clients.
`AllowAny` explicitly accepts every origin. A custom Gorilla
`Upgrader.CheckOrigin` takes precedence over the framework policy.

Defaults are an accept/upgrade capacity of 64, a 5-second header-read timeout,
a 30-second idle timeout, and 8 KiB maximum HTTP headers.

## RSA server keys

`crypto.LoadPEMPrivateKey` accepts RSA PKCS#1 and PKCS#8 PEM only after opening
and statting the path. It rejects non-regular files and any group/world
permission bits (`mode & 0077 != 0`) with `ErrUnsafeKeyPermissions`.

`crypto.SavePEMPrivateKey` writes PKCS#1 PEM, opens with `0600`, explicitly
chmods to `0600`, truncates, and writes. Operators must still protect the
parent directory, backups, process memory, and key rotation procedure.

## Shutdown and delivery

`Serve` owns accepted listeners and connections. Shutdown closes listeners,
cancels connection/session work, waits for the configured grace period, and
then closes remaining transports. Read and write deadlines bound stalled I/O.

`Sender.Push`, `Server.Publish`, and exclusion variants perform semantic,
process-local live delivery through session writers or an enabled
exact-session recovery FIFO. Acceptance into that FIFO is distinct from
physical delivery. These APIs are not a durable or distributed update system.

For session-layer-aware delivery, use `Server.PublishProjected` or its
`Context`, exact-session exclusion, and auth-key exclusion variants. A
`PushProjector` receives the exact eligible `Binding`, including effective
`Layer` and `LeaseGeneration`, and returns the schema object for that session.
Runtime v2 performs no Telegram or application conversion. It encodes each
result under the normal response-size limit and revalidates the registration
before submission. A projector or encoding error fails closed for that target
and is included in the returned joined error; a changed layer, generation,
subscription, or registration returns `ErrPushBindingChanged`. Successful
submission retains the existing copied, bounded recovery FIFO behavior.

`Server.PublishProjectedByAuthKey` uses the same projection and final binding
check while selecting by authorization-key ID rather than positive user ID.
It includes an unauthenticated main session that has subscribed through a
normal request, but excludes non-subscribing file/import sessions. The API is
process-local; applications must retain polling or another durable recovery
path when delivery is missed or the target is connected to another process.
