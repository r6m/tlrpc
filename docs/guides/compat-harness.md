# Compat Test Harness

The compat test harness is a lightweight, in-process server + client pair used by tests to validate:

- MTProto handshake
- Wrapper invocation (`invokeWithLayer` + `initConnection`)
- RPC success and error mapping
- Local push delivery over the active MTProto connection

It is intentionally minimal and uses in-memory managers only. No external services are required.

## What It Includes

- In-memory auth key manager
- In-memory session manager
- Deterministic compat RSA server key
- Minimal RPC surface:
  - `help.getConfig` (success)
  - `help.getNearestDc` (forced error for rpc_error coverage)
  - `auth.signIn` (sets `UserID` to bind updates)

## Tests

The harness is exercised via:

- `compat/harness/harness_test.go`
  - handshake + wrapped RPC
  - rpc_error mapping
- `compat/harness/harness_push_test.go`
  - server-initiated push delivery

Run them directly with:

```
go test ./compat/harness -count=1
```

## Extending It

Keep the harness minimal. It should cover protocol correctness and local push mechanics only. For broader scenarios, use the existing compat scenario suite under `compat/`.
