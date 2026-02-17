# Dispatch Pipeline

A TL method reaches your service handler through these runtime stages:

1. Transport framing (`transport/*`): read/write length-prefixed MTProto frames.
2. MTProto decrypt (`conn.go` + `mtproto/*`): decrypt encrypted payload with auth key.
3. TL decode: read constructor ID, instantiate request object, deserialize fields via server dispatcher (`decodeTLObject`).
4. Wrapper unwrapping: internal wrappers can unwrap `query:!X` to inner request.
5. Method dispatch: lookup handler by constructor ID in `server.dispatcher`.
6. Handler execution: optional unary interceptor chain wraps method handler.
7. TL encode: serialize response TL object.
8. MTProto encrypt/send: encrypt response and write frame.

Container and ACK behavior:

- `msg_container` and related envelopes are represented as typed objects in `mtproto/tl`.
- `msgs_ack` is emitted for processed message IDs.
- `msgs_state_req` and `msg_resend_req` are currently acknowledged without full resend logic.

Server registration path:

- Generated `Register*Server` calls `Server.RegisterService`.
- `RegisterService` binds each method descriptor to `server.dispatcher` using constructor ID + request constructor + handler.
