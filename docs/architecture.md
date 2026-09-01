# Architecture

TLRPC separates an application-owned service API from a reusable protocol
runtime. Generated code is the only boundary between them.

## Static structure

```text
application
  schema.tl
    -> tlrpc-gen
    -> generated types + codecs + ServiceDesc + Register*Server
  service implementations
  auth-key store + session.Store + product stores
                    |
                    v
TLRPC Server / Runtime v2
  transport -> handshake/decrypt -> validate -> normalize -> dispatch
  frame sink <- encrypt/encode <- per-session writer <- outcomes/push
```

The dispatcher maps a method constructor ID from a generated `MethodDesc` to
the registered generated handler. There is no dynamic raw-method registration
and no secondary protobuf/gRPC contract.

## Connection and session ownership

A physical TCP or WebSocket connection owns:

- transport framing and directional deadlines;
- one pinned auth-key identity after encrypted traffic begins;
- global/per-IP/per-auth admission accounting;
- a bounded map of same-auth composite sessions;
- connection-wide application admission; and
- one bounded, serialized physical frame sink.

A composite session, keyed by `(AuthKeyID, SessionID)`, independently owns:

- an exclusive active lease;
- inbound validation and replay state;
- reliability records and protocol controls;
- active request and `invokeAfter` completion tracking;
- routing and application dispatch;
- outbound message IDs, sequence numbers, correlation, and encryption; and
- process-local push subscription state.

Replacing a lease retires only that composite session. A transport or physical
write failure retires the whole connection because its shared byte stream can
no longer be trusted.

## Inbound request flow

```text
bounded carrier frame
  -> authorization-key handshake, or decrypt encrypted envelope
  -> locate/acquire (AuthKeyID, SessionID)
  -> create one shared decode budget
  -> atomically validate envelope and every container child
  -> reserve all callable children as one admission operation
  -> normalize runtime wrappers
  -> route protocol control or generated application method
  -> apply successful named session mutations
  -> submit semantic outbound intents
```

The decode budget follows the logical request through container decoding,
wrapper normalization, gzip expansion, constructor replay, and generated object
decoding. Bytes, wrappers, containers, aggregate vector elements, object nodes,
object depth, gzip ratio, and gzip work are independent dimensions.

If a container cannot reserve all callable children, Runtime v2 emits
correlated `500 SERVER_BUSY` errors and does not commit the candidate inbound
snapshot. No subset of the container starts.

## Durable replay state

The validator works on a candidate copy and commits only after the outer
envelope and every child validate. The resulting detached `session.Snapshot`
contains:

- the highest accepted client message ID;
- a bounded set of recent client message IDs;
- a monotonic `ClientMsgIDFloor`; and
- bounded recent content sequence numbers and directional sequence progress.
- the durable push-subscription opt-in; the active sender is re-bound by the
  new connection and is never persisted.

When the recent-ID window is full, the smallest retained ID is evicted and the
floor advances to at least that value. A later message at or below the floor is
still a replay even though its exact ID is no longer retained. Persistence is
only durable when the configured `session.Store` durably round-trips all these
fields; the default in-memory store is intended for local use.

## `invokeAfter` ordering

`invokeAfterMsg` and `invokeAfterMsgs` are accepted only as the outermost
wrapper. Dependency IDs must be non-zero, earlier than the current request,
unique after normalization, and no more than 64 per request.

Each session keeps bounded completion outcomes for its active-request
capacity. A dependent request waits for every referenced request to complete
successfully. It dispatches only after success, returns `500 MSG_WAIT_FAILED`
when a dependency failed, or returns `500 MSG_WAIT_TIMEOUT` after 500 ms when
an unknown/out-of-order dependency did not become known. Cancellation ends the
wait without dispatch.

## Outbound flow

Handlers return typed results or structured RPC errors. Runtime v2 converts
these to semantic intents. The per-session writer:

1. allocates server message IDs and sequence numbers;
2. creates correlated `rpc_result` or control objects;
3. serializes with the configured encoded-response bound;
4. encrypts and records reliability state;
5. persists outbound protocol progress; and
6. submits a complete frame to the connection sink.

The sink has a bounded queue and one end-to-end write timeout. It serializes
writes from all sessions without merging their protocol ordering state.

## Application state and live delivery

Successful handlers may request explicit user bind/unbind mutations. Named
protocol mutations are persisted before the changed binding is exposed.
Immutable context values provide layer, auth-key ID, client metadata, binding,
and a semantic `Sender`.

`Sender.Send`, `Server.Publish`, and `Server.PublishExceptContext` enqueue
schema-defined objects into target session writers. Delivery is process-local
and best-effort. Durable product updates, recipient policy, and missed-update
recovery remain application responsibilities.

## Observation path

`WithObserver` inserts a typed observer around RPC dispatch, session-store
operations, connection/session lifecycle, admission, and physical writes.
Runtime events enter a bounded internal channel and are delivered by a separate
goroutine. A full channel drops new events; callback panics are recovered.
Observation is diagnostic telemetry, never a synchronization or correctness
boundary.
