# Official Client Compatibility

This project targets framework-level compatibility with official Telegram clients:

- MTProto transports over TCP: abridged, intermediate, padded intermediate, full.
- MTProto over WebSocket with `Sec-WebSocket-Protocol: binary` and obfuscated2.
- Handshake and encrypted message flow with typed MTProto envelopes.
- Wrapper unwrapping (`invokeWithLayer`, `initConnection`, `invokeAfter*`, `invokeWithoutUpdates`) before API dispatch.
- `rpc_result` response envelopes with matching `req_msg_id`.

Automated coverage now includes full handshake integration in `compat/handshake_integration_test.go`:

- `req_pq_multi -> req_DH_params -> set_client_DH_params -> dh_gen_ok`
- transport matrix:
  - TCP abridged
  - TCP intermediate
  - TCP padded intermediate
  - TCP full
  - WebSocket obfuscated2 + padded intermediate
- post-handshake encrypted wrapped request:
  - `invokeWithLayer(initConnection(query=help.getConfig))`
- negative protocol checks:
  - non-monotonic `msg_id` -> `bad_msg_notification`
  - wrong `server_salt` -> `bad_server_salt`
  - malformed encrypted payload fails without server crash

## Compatibility Harness

Use `cmd/compat-server` for real-client probing:

```bash
go run ./cmd/compat-server --tcp 127.0.0.1:8080 --ws 127.0.0.1:8081 --max-layer 217 --trace=true
```

Behavior:

- Runs TCP and WebSocket listeners on separate ports.
- Logs constructor ID, method name, TL name, layer, user ID, auth key ID.
- Implements minimal startup surface for `help`, `updates`, `users`, `auth`.
- Returns MTProto `rpc_error` for unimplemented methods (does not crash the connection).

## Notes

- Runtime dispatch relies on typed constructors from `mtproto/tl`; `conn.go` does not hardcode constructor IDs.
- Generated API code is restricted to API-layer types; MTProto envelope objects remain in `mtproto/tl`.
- This is intentionally not a full Telegram backend. It is a compatibility-oriented framework baseline.
