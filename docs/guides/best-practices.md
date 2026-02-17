# Best Practices

- Keep schema and generated code versioned together.
- Register all services at startup, fail fast on registration errors.
- Use `WithUnaryInterceptor` for auth/logging/recovery/metrics.
- Return explicit MTProto error messages (`PHONE_NUMBER_INVALID`, etc.).
- Keep handler logic stateless where possible; persist session/business state explicitly.
- Validate every request field before downstream calls.
- Set up idempotency/dedupe based on session + message IDs for side-effecting operations.
- Load test with container (`msg_container`) traffic patterns, not only single RPC calls.
