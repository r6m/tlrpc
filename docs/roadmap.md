# Roadmap

Runtime v2 is the only architecture. This roadmap starts after the
v0.12.0 Production Readiness release and lists only unfinished work; it is not
a plan to retain an older runtime or compatibility shims.

## v0.12.0 Production Readiness release

The implementation target is complete when the following release gate passes
against the final commit:

1. Regenerate the custom schema and Telegram layer-228 fixtures twice and prove
   byte-identical checked-in output.
2. Compile generated packages and examples.
3. Run focused parser/generator, replay, `invokeAfter`, codec/gzip/response
   budget, observer, quota, WebSocket-origin, RSA-key, lifecycle, and malformed
   input tests.
4. Run complete Runtime v2 TCP and WebSocket conformance, including independent
   client startup, reconnect, RPC, push, controls, and replay recovery.
5. Run all package tests, race tests, vet, builds, formatting, module-tidy, and
   architecture guards sequentially with bounded Go compilation parallelism.
6. Verify documentation and exported API names describe one framework model and
   contain no superseded current-release claims.
7. Record known limitations, tag v0.12.0, and publish only after the gate is
   green and publication is authorized.

Release evidence must distinguish unit coverage from real network and durable
store integration. The existence of code or a consumer-specific test is not by
itself release evidence.

## After v0.12.0

Prioritize future work only when a generic framework consumer demonstrates the
need:

- benchmark and allocation budgets for production-shaped schemas, nested
  containers, gzip payloads, and multi-session connections;
- observer drop accounting or configurable exporters without placing callbacks
  on the protocol path;
- clearer structured delivery reports for process-local publish;
- additional MTProto-compatible carriers behind the same bounded transport
  contract; and
- multi-process presence/fanout integration points that preserve application
  ownership of durable update semantics.

## Permanent non-goals without a new framework proposal

The following remain application concerns:

- Telegram product services and schema policy;
- databases, queues, object stores, migrations, retention, and backups;
- durable update logs, difference APIs, and recipient selection;
- HTTP/JSON Bot API routes; and
- deployment topology and tenant policy.

## Production-foundation definition

TLRPC is a production protocol foundation—not a complete product platform—when
arbitrary schemas generate and dispatch through typed services, protocol state
is durable and single-owner, malformed input and resource consumption are
bounded, supported carriers have independent-client evidence, observation does
not affect correctness, and consumers can keep every product semantic outside
the framework.
