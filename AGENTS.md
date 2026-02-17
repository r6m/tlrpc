# Agent Instructions (TLRPC)

## Mission
Fix project structure + docs so they match the actual code, and ensure `go test ./...` passes.

## Ground Truth
- Code is authoritative. Docs must match exported Go API and runtime behavior.
- MTProto/TL edge server with gRPC-like handlers.
- Wrapper methods (invokeWithLayer/initConnection/etc.) are internal forwarding, not user services.

## Non-goals
- No protobuf/gRPC transport layer.
- No public API breaking changes without compatibility shims.

## Required workflow
1) Read: README.md, API.md, SCHEMA.md, server.go, conn.go, dispatcher.go, session/*, internal/generator/*
2) Make small, reviewable commits.
3) After each major step run:
   - gofmt ./...
   - go test ./...
   - go vet ./...

## Acceptance criteria
- `go test ./...` passes.
- README points to docs/index.md.
- Docs are not duplicated/contradictory; one source of truth under /docs.
- Dispatch pipeline is documented end-to-end.
- Registration story is consistent: method constructor ID -> handler exists at runtime.
- conn.go dispatch calls handlers even if no interceptors are configured.

## Style rules
- Prefer minimal, backward-compatible changes.
- If moving docs, use git mv.
- Update links after moves.