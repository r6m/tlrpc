# Roadmap

Runtime v2 is the current architecture. This roadmap contains unfinished work,
release evidence, and later consumer adoption; it is not a migration plan for
keeping the old runtime alive. TLRPC has no released compatibility contract,
so superseded code and APIs are removed rather than deprecated or shipped in
parallel.

## Implemented foundation

The following are current, not roadmap proposals:

- schema-neutral TL parsing and deterministic Go generation;
- base-schema plus repeatable ordered `--layer-diff=<layer>:<path>` resolution
  for a selected `--layer`, including same-name replacement, additions, and
  the exact `// @tlrpc remove constructor NAME` and
  `// @tlrpc remove function NAME` directives;
- generated gRPC-like service contracts and static registration descriptors;
- a custom non-Telegram acceptance schema;
- exact Telegram layer-228 generation as a separate compatibility fixture;
- one Runtime v2 server connection path for TCP and WebSocket;
- bounded transport framing and bounded nested MTProto decoding;
- server-owned authorization-key handshake and encryption;
- detached `session.Store` snapshots and exclusive composite-session leases;
- explicit inbound/outbound runtime contracts;
- one auth key per physical connection and a bounded same-auth composite-session
  map, with a default capacity of 16;
- independent per-session validation, routing, active requests, reliability,
  writer, push subscription, and outbound persistence;
- per-session protocol ordering behind one connection-owned serialized,
  non-closing physical frame sink, with connection-wide request admission;
- matching-session-only retirement when a lease is replaced;
- wrappers and controls, including composite-session-local
  `invokeWithoutUpdates` subscription;
- immutable request metadata, explicit user binding, semantic `Sender`, and
  process-local `Server.Publish`;
- direct replacement of the mixed legacy runtime and removal of its public
  compatibility APIs.

## Completed gate 1: First-release conformance

1. Finish the authoritative Runtime v2 protocol matrix for malformed frames,
   encrypted envelopes, salts, session identity, message IDs, directional
   sequence numbers, wrappers, containers, correlation, acknowledgements,
   resend/state/drop-answer, persistence, reconnect, push, cancellation, and
   shutdown.
2. Run every carrier invariant over TCP and WebSocket where applicable.
3. Run independent gotd startup, reconnect, RPC, and push scenarios rather
   than relying only on round trips through TLRPC's compatibility client.
4. Run race and resource-limit checks sequentially with bounded Go build
   parallelism.
5. Keep architecture checks that reject raw registration, mutable sessions,
   direct connection access, old runtime files, and physical writes outside
   the connection-owned frame sink.

Gate: all documented Runtime v2 behavior has current, specific evidence and no
test depends on a removed compatibility path.

## Completed gate 2: Local release readiness

1. Regenerate all checked-in fixtures and prove deterministic diffs.
2. Run unit, integration, malformed-input, race, lifecycle, architecture, TCP,
   WebSocket, and independent-client suites with controlled compiler
   concurrency.
3. Verify examples and compatibility tools against the final public API.
4. Audit exported names and documentation so only the Runtime v2 model is
   described or reachable.
5. Produce a release-readiness report with known limitations.
6. Update release notes, tag, and publish only with explicit authorization.

Gate: the first release is one coherent framework contract, with no deprecated
flags, shims, dual runtime, or hidden legacy build path.

Local evidence recorded on 2026-08-30:

- checked-in framework and echo fixtures are byte-identical after a second
  regeneration pass, and both generated packages compile;
- `go test -count=1 -p=1 ./...`, `go vet -p=1 ./...`, sequential lint, and
  `go test -race -p=1 ./...` pass;
- the tagged gotd transport suite and TCP/WebSocket scenario suite pass;
- legacy, type-domain, direct-transport, and product-semantics architecture
  guards pass; and
- `go mod tidy`, all-package build, formatting, and diff checks are clean.

Known limitations remain intentionally outside the v0.8 framework contract:
process-local publish is neither durable nor distributed, additional carriers
are future extensions, and product update history, fanout, databases, queues,
object storage, HTTP Bot API behavior, and deployment policy remain application
responsibilities. TLRPC v0.8.0 was published on 2026-08-30 after explicit
authorization.

## Gate 3: Adopt released TLRPC in `tgserver`

This is consumer work, not part of defining TLRPC complete.

1. Generate `tgserver` contracts from its exact selected Telegram schema.
2. Implement/register those generated service servers through released TLRPC
   APIs.
3. Provide durable auth-key storage and a durable `session.Store` keyed by
   `(AuthKeyID, SessionID)` that persists every snapshot field.
4. Route `tgserver` TCP and WebSocket listeners through Runtime v2.
5. Use explicit user binding, semantic sender, and local publish while keeping
   durable update logs, outboxes, difference APIs, and recipient policy in
   `tgserver`.
6. Prove authentication, dialogs, messages, media, live updates, reconnect,
   logout/revocation, and missed-update recovery with real clients.
7. Remove `tgserver`'s old gateway; do not retain a dual-stack fallback.

Gate: `tgserver` uses TLRPC as its generic protocol framework without TLRPC
absorbing Telegram product semantics or deployment policy.

Current state: tgserver's source is generated at layer 228, implements and
registers product services through Runtime v2, uses explicit binding and
semantic publish, and includes a PostgreSQL `session.Store` for every detached
snapshot field. Runtime v2 is now its sole TCP/WebSocket gateway; the previous
gateway, router, MTProto, transport, crypto, and in-process gRPC bridge have
been deleted. The full suite plus real-gotd authentication, live delivery,
reconnect, and missed-update recovery gates pass against the published v0.8.0
module with no workspace replacement.

## Later framework work

These items may follow the first release when driven by concrete consumers:

- additional MTProto-compatible carriers behind the bounded transport
  contract;
- richer observability around leases, per-session writer pressure,
  connection-wide admission, reliability retention, and protocol failures
  without exposing secrets;
- clearer process-local delivery reports;
- performance profiling and allocation budgets against production-shaped
  schemas and traffic.

Distributed presence, cross-process fanout, durable application updates,
databases, queues, object stores, and HTTP Bot API routes remain consuming
application concerns unless a future proposal establishes a genuinely generic
framework contract.

## Definition of done

TLRPC is ready as a production protocol foundation when arbitrary schemas
generate and dispatch through typed services, Runtime v2 has independent-client
and malformed-input evidence across supported carriers, protocol state is
durable and single-owner, resource limits and shutdown are bounded, and
applications can retain all durable product semantics outside the framework.
