# Echo example

This example generates a tiny TL schema and starts an MTProto-capable server that echoes a string payload.

## Regenerate

```
GOCACHE=/tmp/go-build go run ./cmd/tlrpc-gen -schema=examples/echo/schema.tl -out=examples/echo/gen -package=echo
```

## Run

```
go run ./examples/echo
```

The server listens on `:9000` using the TCP transport.
