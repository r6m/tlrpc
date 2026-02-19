# Architecture

This document describes the request lifecycle and where extensions should hook into the runtime without modifying core dispatch.

## Request Lifecycle

1) **Accept connection**
- `Server.Serve` / `Server.ServeTransport` accept a transport connection.
- A `connHandler` is created per connection.

2) **Read + decrypt**
- `connHandler.run` reads raw MTProto frames via `conn.ReadMessage()`.
- `connHandler.processMessage` routes unencrypted vs encrypted.
- `connHandler.handleEncryptedMessage` decrypts to `mtproto.InnerData`.

3) **Session + context**
- Auth key and session are loaded (`authKeys.Get`, `sessions.Get`/`Create`).
- `withSession`, `withAuthKeyID`, `withLayer`, `withUserID` populate request context.

4) **Decode + dispatch**
- `decodeTLObject` converts bytes into TL objects.
- `dispatchDecodedObject` unwraps envelopes (`invokeWithLayer`, `initConnection`, `invokeAfter*`, `invokeWithoutUpdates`).
- Handler dispatch goes through the server dispatcher.

5) **Encode + write**
- Response objects are TL-encoded (`encodeTLObject`) and wrapped in `rpc_result`.
- `mtproto.InnerData` is encrypted and written to the connection.
- Acks are sent for received message IDs.

## Hook Points

### Response Normalization
- **Where:** immediately before `encodeTLObject` in `connHandler.handleEncryptedMessage` and container handling.
- **Why:** normalize handler outputs (pointer-to-interface, slices) so apps don't need custom interceptors.

### Connection-in-Context
- **Where:** after session/context setup in `handleEncryptedMessage`.
- **Goal:** expose a stable `Conn` handle via `context.Context` for local push.

### Session Bind/Unbind Hooks
- **Where (bind):** after a session becomes authorized and a connection is associated with a user/session.
- **Where (unbind):** on connection close (or context done).
- **Goal:** allow apps to publish presence / routing events externally (Redis/NATS) without core dependencies.

### Local Push Path
- **Where:** a helper that encrypts and writes TL objects to the current connection using the active auth key and session binding.
- **Scope:** local only (current connection), no fanout or distributed routing.

## Existing Protocol Helpers

- `mtproto.MsgIDGenerator` and `mtproto.SeqNoGenerator` drive server-sent message IDs and sequence numbers.
- MTProto envelope encoding/decoding is under `mtproto/` and `mtproto/tl/`.

## Extension Design Goals

- Keep core small and transport-agnostic.
- Expose interfaces/hooks rather than embedding external systems.
- Ensure compatibility tests cover handshake, wrappers, and local push basics.
