# Custom Client Harness

This repo ships a small MTProto v2 developer client (`tlrpc-client`) that can talk to the local compat server without patching official Telegram apps. It exercises the same transport matrix as the compat tests.

The compat server is a protocol harness only. It implements a minimal boot surface (`help.getConfig`, `updates.getState`, `updates.getDifference`, plus auth/users stubs) and intentionally does not provide real Telegram semantics (dialogs, message history, messaging).

## Run the compat server

```bash
go run ./cmd/compat-server --tcp :9000 --ws :9001
```

The compat server embeds a fixed RSA server key for the MTProto handshake. `tlrpc-client` uses the same key.
This is not a production server and is intentionally limited to protocol/boot-ritual compatibility.

## Run tlrpc-client (TCP)

Abridged:

```bash
go run ./cmd/tlrpc-client ping-config --tcp :9000 --codec abridged
```

Intermediate:

```bash
go run ./cmd/tlrpc-client ping-config --tcp :9000 --codec intermediate
```

Padded intermediate:

```bash
go run ./cmd/tlrpc-client ping-config --tcp :9000 --codec padded
```

Full:

```bash
go run ./cmd/tlrpc-client ping-config --tcp :9000 --codec full
```

## Run tlrpc-client (WebSocket)

WebSocket uses obfuscated2 + padded intermediate internally.

```bash
go run ./cmd/tlrpc-client ping-config --ws ws://localhost:9001/
```

## Generic invoke

Currently supported:

```bash
go run ./cmd/tlrpc-client invoke --method help.getConfig --tcp :9000 --codec abridged
```

For auth flows, use the dedicated subcommands:

```bash
go run ./cmd/tlrpc-client auth-sendcode --phone +15551234567 --api-id 77777 --api-hash devhash --tcp :9000 --codec abridged
```

```bash
go run ./cmd/tlrpc-client auth-signin --phone +15551234567 --code 12345 --code-hash <hash> --tcp :9000 --codec abridged
```

## Common troubleshooting

- Handshake failures (`unexpected EOF` or `unknown constructor`): use `cmd/compat-server`, use only one transport flag (`--tcp` or `--ws`), and ensure the TCP codec matches the server.
- `bad_msg_notification`: client msg IDs are time-based; a system clock skew >30 seconds will be rejected.
- `bad_server_salt`: the client retries once automatically. Repeated failures usually indicate mismatched session/auth state.
