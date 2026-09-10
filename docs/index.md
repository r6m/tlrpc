# TLRPC documentation

TLRPC turns an application-owned TL schema into typed Go service contracts and
serves their implementations through one reusable MTProto Runtime v2. It is a
generic framework: no Telegram API schema, layer, product workflow, database,
or companion service is mandatory.

```text
.tl schema -> tlrpc-gen -> generated contracts -> application services
                                                 |
TL/MTProto client <- TCP or WebSocket <- Runtime v2
```

Each page has one responsibility:

1. [Requirements](./requirements.md) is the normative framework/application
   boundary and current source-tree contract.
2. [Architecture](./architecture.md) explains ownership and the end-to-end
   Runtime v2 request, session, replay, and write paths.
3. [Implementation](./implementation.md) documents current public APIs,
   configuration, defaults, and operational behavior.
4. [Telegram and MTProto](./telegram-mtproto.md) covers Telegram-specific wire
   behavior and the role of the layer-228 fixture.
5. [Roadmap](./roadmap.md) contains only unfinished work after v0.12.0 and the
   release gate.

Historical release changes are in [CHANGELOG.md](../CHANGELOG.md).

The [distinct layer contracts refactor](./distinct-layer-contracts-plan.md)
defines stable boxed-type interfaces, source-compatible concrete initializers,
generated codec dispatch, strict method lifetimes and consumer migration gates.
Framework implementation and validation are complete in the working tree;
tgserver regeneration and migration are a separate follow-up.

## Scope at a glance

TLRPC provides schema parsing/generation, generated registration, MTProto
handshake and encryption, composite protocol sessions, bounded decoding and
writes, protocol wrappers and controls, typed observation, and process-local
live push.

Applications provide service behavior, user authentication and authorization,
durable product state, recipient policy, durable update logs/difference APIs,
cross-process fanout, storage, HTTP APIs, and deployment policy.

The [codec performance plan](./codec-performance-plan.md) records the implemented
codec, dispatch and buffer-ownership improvements. The
[performance report](./performance/README.md) includes measured gains, tradeoffs,
raw results, and the zero-copy decision.
