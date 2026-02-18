# Dispatch Pipeline

A TL method reaches your service handler through these runtime stages:

1. Transport framing (`transport/*`): read/write length-prefixed MTProto frames.
2. MTProto decrypt (`conn.go` + `mtproto/*`): decrypt encrypted payload with auth key.
3. TL decode: read constructor ID, instantiate request object, deserialize fields via server dispatcher (`decodeTLObject`).
4. Wrapper unwrapping: internal wrappers can unwrap `query:!X` to inner request.
5. Session side effects: wrapper processing updates runtime state (layer, init metadata) before inner dispatch.
6. Method dispatch: lookup handler by constructor ID in `server.dispatcher`.
7. Handler execution: optional unary interceptor chain wraps method handler.
8. TL encode: serialize response TL object.
9. RPC envelope: wrap method responses/errors as `rpc_result(req_msg_id, result)`.
10. MTProto encrypt/send: encrypt response and write frame.

Container and ACK behavior:

- `msg_container` and related envelopes are represented as typed objects in `mtproto/tl`.
- `msgs_ack` is emitted for processed message IDs.
- `msgs_state_req` and `msg_resend_req` return minimal `msgs_state_info` responses.
- `gzip_packed` is inflated before decode and dispatch.

Server registration path:

- Generated `Register*Server` calls `Server.RegisterService`.
- `RegisterService` binds each method descriptor to `server.dispatcher` using constructor ID + request constructor + handler.
