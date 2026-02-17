# Updates

There are two update delivery patterns in MTProto systems:

- Inline updates: returned in method responses.
- Pushed updates: delivered asynchronously on active sessions.

Project status:

- The framework handles request/response dispatch and container/ack mechanics.
- A complete pushed-updates subsystem is not implemented yet (fanout/state synchronization policy remains application-specific).

Recommended strategy today:

- Return required state changes inline from method responses.
- If you need push semantics, implement application-level fanout and per-session delivery tracking outside the core runtime.
