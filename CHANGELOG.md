# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Released entries are historical records; the Unreleased section and current
documentation define future framework behavior.

## [Unreleased]

## [0.12.1] - 2026-09-01

### Fixed

- Return `dh_gen_retry` when a Diffie-Hellman exchange produces a short
  minimal shared secret, preserving the temporary exchange state and enforcing
  the retry ID before accepting a full 2048-bit authorization key.
- Accept future even non-content sequence numbers without advancing durable
  content progress, allowing acknowledgements to arrive ahead of content sent
  through another connection in the same MTProto session.
- Prevent the compatibility client from proposing short authorization keys.

## [0.12.0] - 2026-08-31

Production Readiness for the generic TL-schema-first framework. The
features below are schema-neutral; Telegram layer 228 and `tgserver` are
compatibility consumers, not framework dependencies.

### Added

- Execute `invokeAfterMsg` and `invokeAfterMsgs` as real per-session ordering
  dependencies. Dependencies must be outermost, valid earlier message IDs, are
  deduplicated and capped at 64, and wait at most 500 ms. Successful
  dependencies release dispatch; failed dependencies return
  `500 MSG_WAIT_FAILED`; unresolved dependencies return
  `500 MSG_WAIT_TIMEOUT`.
- Persist a monotonic client message-ID replay floor and recent content
  sequence numbers in `session.Snapshot`. The floor advances as the bounded
  exact-ID window evicts its minimum, so old IDs cannot execute again after
  reconnect or restart when the configured store durably preserves the full
  snapshot.
- Add one shared logical-request decode budget covering aggregate decoded
  bytes, wrappers, containers, vector elements, generated object nodes/depth,
  gzip expansion ratio, and decompression work.
- Bound generated response serialization before encryption with
  `MaxEncodedResponseBytes`, and bound the connection-owned physical write
  queue with one end-to-end write deadline.
- Extend `ResourceLimits` with decoded-object, gzip, response, physical queue,
  global connection, per-IP, per-auth-key, and per-connection session limits.
- Add `WithObserver` and typed connection, handshake, session, RPC, admission,
  writer, store, and gauge events. Delivery is non-blocking through a bounded
  internal queue and observer panics are isolated from Runtime v2.
- Add `WebSocketOriginPolicy` with same-origin defaults, explicit allowlists,
  missing-origin policy, and explicit `AllowAny`; also bound WebSocket upgrade
  admission, HTTP header size, header-read timeout, and idle timeout.
- Add RSA private-key file protections: loading rejects non-regular or
  group/world-accessible files, and saving enforces mode `0600`.

### Changed

- Make inbound container validation and active-request reservation atomic: all
  children are accepted or none start, and overload returns correlated
  `500 SERVER_BUSY` responses without consuming the candidate protocol
  snapshot.
- Share decode budgets across nested wrapper, container, gzip, constructor
  replay, and generated object decoding instead of granting each layer a new
  allowance.
- Use one connection-owned frame sink as the sole serialized, bounded physical
  writer after handshake; session writers retain independent protocol ordering.
- Treat panic recovery as a mandatory runtime boundary. The recovery
  interceptor no longer exposes the former customization shape; unknown and
  panic errors are sanitized while intentional structured RPC errors survive.
- Define WebSocket upgrades as `GET` requests requiring the `binary`
  subprotocol and obfuscated2. Cross-origin browser deployments must configure
  an explicit allowlist.
- Remove synchronous `WithOnSessionBound` and `WithOnSessionUnbound` callbacks.
  Typed non-blocking `SessionEvent` observation is the only lifecycle
  notification surface, so user callbacks cannot stall connection shutdown.

### Security

- Prevent replay-window eviction from reopening old client message IDs when a
  durable store round-trips the new snapshot fields.
- Prevent decompression bombs, aggregate nested allocation escape, oversized
  generated responses, unbounded physical write backlog, and excessive
  connection/session admission through independent limits.
- Prevent accidental loading of private RSA keys from unsafe file types or
  group/world-readable paths and prevent framework saves from retaining a
  permissive existing mode.

### Documentation

- Reframe TLRPC consistently as a generic framework driven by an
  application-owned TL schema; `tgserver` is a consumer and layer 228 is a
  fixture.
- State explicitly that layer differences are resolved only during generation
  and Runtime v2 never translates API layers.
- Consolidate requirements, architecture, concrete implementation/defaults,
  Telegram behavior, and unfinished roadmap work into non-overlapping pages.

## [0.11.2] - 2026-08-31

### Fixed

- Classify `gzip_packed` requests as content-related so sequence validation
  matches the wrapped application request.

## [0.11.1] - 2026-08-31

### Fixed

- Accept the already-advanced first content sequence number of a restored
  session that has no persisted client progress.

## [0.11.0] - 2026-08-31

### Added

- Support multiple same-auth composite MTProto sessions on one physical
  connection with independent session ownership and writers.

## [0.10.2] - 2026-08-31

### Fixed

- Retain all parallel session subscribers instead of replacing another
  session's live-push registration.

## [0.10.1] - 2026-08-31

### Fixed

- Preserve `invokeWithoutUpdates` subscription state correctly.

## [0.10.0] - 2026-08-30

### Added

- Add process-local publish that can exclude the exact originating composite
  session.

## [0.9.0] - 2026-08-30

### Changed
- Replace the four gRPC-shaped `WithMaxMessageSize`,
  `WithMaxConcurrentStreams`, `WithReadTimeout`, and `WithWriteTimeout`
  options with one TL-native `WithResourceLimits(ResourceLimits)` policy. The
  old exports are removed without aliases.
- Require transport connections to expose independent read and write deadlines,
  making the Runtime v2 resource policy enforceable for custom transports.

### Fixed
- Accept standard PKCS#8 RSA private-key PEM files in addition to PKCS#1 files
  when loading an MTProto server key.

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
- Consolidate documentation into canonical requirements, Telegram/MTProto,
  architecture, implementation, and roadmap pages.
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
