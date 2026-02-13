# Contributing to TLRPC

## Development Setup

```bash
git clone https://github.com/r6m/tlrpc.git
cd tlrpc
make deps
make test
```

## Project Structure

- `tlrpc` (repo root package): Core framework - stable API
- `transport/`: Transport implementations
- `crypto/`: Cryptographic primitives
- `session/`: Session management
- `mtproto/`: MTProto protocol
- `layer/`: Layer support
- `codec/`: TL encode/decode registry
- `internal/codegen/`: Code generation library
- `cmd/tlrpc-gen/`: CLI tool

## Code Standards

- **Go Version**: 1.24+
- **Linting**: `golangci-lint` with strict config
- **Testing**: >80% coverage for core, >90% for crypto
- **Documentation**: All exported types documented
- **Commits**: Conventional commits format

## Adding a New Transport

1. Implement `Transport`, `Listener`, `Conn` interfaces
2. Add to `transport/yourtransport.go`
3. Add tests with `transport_test.go` pattern
4. Update documentation

## Adding Code Generation Features

1. Modify `internal/codegen/generator.go`
2. Add template to `internal/codegen/templates.go`
3. Regenerate test files: `make generate-test`
4. Verify with `make test`

## Testing

```bash
make test          # Unit tests
make test-race     # Race detector
make test-integration  # Integration tests
make bench         # Benchmarks
```

## Release Process

1. Update CHANGELOG.md
2. Tag version: `git tag vX.Y.Z`
3. Push: `git push origin vX.Y.Z`
4. CI builds and publishes

## Architecture Decisions

Document significant decisions in `docs/adr/`.
