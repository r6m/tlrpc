# Agent Instructions (TLRPC)

## Mission
Fix project structure + docs so they match the actual code, and ensure `go test ./...` passes.

## Ground Truth
- Code is authoritative. Docs must match exported Go API and runtime behavior.
- MTProto/TL edge server with gRPC-like handlers.
- Wrapper methods (invokeWithLayer/initConnection/etc.) are internal forwarding, not user services.

## Non-goals
- No protobuf/gRPC transport layer.
- TLRPC has no released compatibility contract yet. Prefer one coherent final
  API and delete obsolete APIs/adapters instead of adding compatibility shims.

## Required workflow
1) Read: README.md, docs/index.md, docs/requirements.md,
   docs/telegram-mtproto.md, docs/architecture.md, docs/implementation.md,
   docs/roadmap.md, server.go, runtime_application.go, internal/runtime/*,
   dispatcher.go, session/*, internal/generator/*
2) Keep changes focused and reviewable.
3) After each major step run sequentially (`-p=1`) to avoid compiler pressure:
   - gofmt ./...
   - go test ./...
   - go vet ./...

## Acceptance criteria
- `go test ./...` passes.
- README points to docs/index.md.
- Docs are not duplicated/contradictory; one source of truth under /docs.
- Dispatch pipeline is documented end-to-end.
- Registration story is consistent: method constructor ID -> handler exists at runtime.
- Runtime v2 dispatch calls generated service handlers with or without interceptors.

## Style rules
- Prefer focused, reviewable changes that converge on the final design; do not
  retain legacy code solely for backward compatibility.
- If moving docs, use git mv.
- Update links after moves.
