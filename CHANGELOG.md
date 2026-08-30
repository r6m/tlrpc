# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.8.0] - 2026-08-30

This release establishes the first supported framework API.
Runtime v2 uses the composite keyed-session contract and removes superseded
legacy contracts rather than preserving backward compatibility.

### Added
- Schema-neutral `BindSessionUser` and `UnbindSessionUser` APIs with guaranteed
  post-handler keyed-session persistence.
- Exact Telegram-style schema support for primitive pseudo-declarations,
  implicit function IDs, repeated sections, and serializer-prefix helpers;
  the unmodified `tgserver` layer-228 schema now generates and compiles.
- Deterministic generated provenance headers containing the selected layer and
  exact schema SHA-256.
- Generated `SchemaLayer` metadata so consumers can identify the schema
  snapshot represented by a package without introducing runtime layer policy.
- Generation-only layer resolution from a base schema labeled by
  `--base-layer`, repeatable ordered `--layer-diff=<layer>:<path>` fragments,
  and a target `--layer`. Delta fragments use ordinary TL declarations;
  same-name declarations replace, new declarations add, and the exact
  `// @tlrpc remove constructor NAME` and
  `// @tlrpc remove function NAME` directives remove declarations without
  runtime layer conversion.
- Static generated `ServiceDesc`/`MethodDesc` registration and a framework-first
  custom-schema acceptance test independent of Telegram and `tgserver`.
- Composite `(AuthKeyID, SessionID)` storage, canonical encrypted-message
  validation, implementation-neutral MTProto conformance tests, and bounded
  handshake state.
- Session-scoped acknowledgement, message-state, and resend tracking with
  configurable capacity/TTL bounds.
- Graceful listener/connection ownership, message-size limits, directional
  deadlines, handler-concurrency limits, and synchronous serialized write
  backpressure.
- An encoding-neutral `session.Snapshot` and `session.Store` contract,
  including all wire-critical directional progress and explicit copy/concurrency
  semantics.
- Built-in `rpc_drop_answer` control handling so client cancellation remains an
  MTProto runtime concern rather than leaking into generated application schemas.
- Opt-in real-gotd reconnect and server-push-before-RPC-result compatibility
  gates over generated services.

### Changed
- Code generator now emits unified/sum types as interfaces (no pointer-to-interface). RPC methods return the interface directly for sum types, and pointers only for concrete struct results. Vectors of concrete types now use slices of pointers.
- Documentation is consolidated into six canonical pages covering requirements, Telegram TL/MTProto, architecture, implementation, and the staged `tgserver` adoption roadmap, with current behavior separated from production requirements.
- Incoming `msgs_ack` is consumed without acknowledgement-of-acknowledgement;
  content classification and sequence validation now follow MTProto rules.
- The generated compatibility package is tracked so downstream `go mod tidy`
  does not reference release-omitted test fixtures.

### Fixed
- Connection shutdown now propagates the actual transport or cancellation cause
  instead of reporting every ordinary disconnect as an invalid protocol
  transition; offline reconnect conformance waits for the exact composite
  session to unbind before testing missed-update recovery.
- Use the canonical first server content sequence number (`1`) and persist
  independent client/server directional sequence progress across reconnects.
- Queue server-initiated objects that arrive while the compatibility client is
  waiting for an RPC result, making RPC and live-update ordering race-safe.
- Normalize generated scalar `bool` service responses to canonical TL
  `boolTrue`/`boolFalse` constructors before wire encoding.
- Remove server-push delivery bindings immediately when an application unbinds
  the active user session.
- `crypto.AuthKey.ID` now uses Telegram's canonical last-eight-bytes SHA-1
  derivation, matching the handshake and encrypted-message runtime.
- Generated and PEM-loaded RSA server keys now use Telegram's canonical
  TL-serialized public-key fingerprint, matching gotd and official clients.
- Protocol progress, salt, wrapper metadata, and new-session notification
  persistence failures now fail the active request/connection instead of
  silently continuing with state that cannot survive reconnect; final
  disconnect-save failures are logged.
- `Server.Publish` now aggregates failures from online session deliveries and
  retires connections whose auth key, reliability state, write, or outbound
  sequence persistence failed, while publishing to an offline user remains a
  successful no-op.
- Responses to `msg_container` now emit child `rpc_result` objects referencing
  each child request ID and send the response container directly, rather than
  incorrectly wrapping it in an `rpc_result` for the outer container ID.
- Outbound message-ID/sequence allocation now shares the serialized write
  critical section, preserving wire order when RPC responses and concurrent
  live pushes race.

## [0.1.0] - 2026-02-18

### Added
- MTProto v2 handshake coverage (`req_pq_multi` -> `req_DH_params` ->
  `set_client_DH_params` -> `dh_gen_ok`).
- Transport compatibility matrix: TCP abridged/intermediate/padded/full and
  WebSocket obfuscated2 + padded intermediate.
- Wrapper/envelope pipeline support: `invokeWithLayer`, `initConnection`,
  `invokeAfter*`, `invokeWithoutUpdates`, plus `msg_container`, `gzip_packed`,
  `rpc_result`, and `msgs_ack`.
- Compatibility harness tooling: `cmd/compat-server`, `cmd/tlrpc-client`, and
  `compat/client`.
- Guardrails: `check_type_domains.sh`, `check_no_legacy.sh`, and
  `check_no_semantics_creep.sh`.
