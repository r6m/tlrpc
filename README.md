# tlrpc

TLRPC is a Go framework for building Telegram MTProto/TL RPC servers with generated service bindings.

## Quick start

```bash
go install github.com/r6m/tlrpc/cmd/tlrpc-gen@latest
go install github.com/r6m/tlrpc/cmd/tlrpc-client@latest
tlrpc-gen --schema=./schema.tl --out=./gen --package=gen
```

```go
srv := tlrpc.NewServer()
gen.RegisterAuthServer(srv, &AuthService{})
log.Fatal(srv.Serve(listener))
```

## Documentation

See [docs/index.md](./docs/index.md).

## Versioning / Releases

- Tags are required for `@latest` (`v0.x` releases are considered unstable; APIs may change).
- See [CHANGELOG.md](./CHANGELOG.md) for release notes.
