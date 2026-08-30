# TLRPC Documentation

TLRPC turns an application-owned TL schema into typed Go objects, codecs, and
gRPC-like service contracts, then connects MTProto clients to implementations
of those contracts. It is a general framework: no Telegram API schema, layer,
service, or product workflow is required.

```text
application TL schema
  -> tlrpc-gen
  -> generated types, codecs, service interfaces, and registration helpers
  -> application service implementations
  -> Runtime v2 over TCP or WebSocket
  -> TL/MTProto client
```

Read these documents in order:

1. [Requirements](./requirements.md) defines the framework contract and the
   boundary between TLRPC and consuming applications.
2. [Architecture](./architecture.md) explains Runtime v2 ownership, request
   flow, session persistence, and server push.
3. [Implementation](./implementation.md) shows the current generator and
   public application-facing APIs.
4. [Telegram and MTProto](./telegram-mtproto.md) documents the optional MTProto
   behavior and the role of the layer-228 fixture.
5. [Roadmap](./roadmap.md) lists only unfinished framework work, release gates,
   and the separate `tgserver` publication/cutover work.

## Current

The current architecture provides:

- arbitrary-schema TL parsing and deterministic Go generation;
- generation-time resolution from a base schema plus ordered, repeatable layer
  differences, with explicit additions, replacements, and removals;
- generated service interfaces, descriptors, unimplemented stubs, and
  registration helpers;
- a single Runtime v2 connection path—there is no legacy runtime selector;
- bounded TCP and WebSocket framing and bounded nested MTProto decoding;
- server-owned handshake, auth-key lookup, encryption, validation, wrappers,
  containers, and protocol controls;
- exclusive `(AuthKeyID, SessionID)` leases over detached `session.Snapshot`
  values stored through `session.Store`;
- one writer per connection for outbound ordering, IDs, sequence numbers,
  correlation, batching, encryption, reliability, and persistence;
- immutable request metadata, explicit user-binding mutations, semantic
  `Sender`, and process-local `Server.Publish`.

## Not part of the architecture

There is no mutable public session, pointer-retaining session manager, raw
connection in handler context, direct method/constructor registration,
deprecated runtime flag, compatibility adapter, or parallel legacy pipeline.

Telegram layer 228 is an exact compatibility fixture. `tgserver` now uses
Runtime v2 as its sole protocol gateway with a durable composite store. It is
a released-module consumer and does not change the framework boundary.
