# Errors

TLRPC uses `RPCError` for MTProto-compatible error responses.

```go
type RPCError struct {
    ErrorCode    int32
    ErrorMessage string
}
```

Helpers:

- `NewRPCError(code int32, message string)`
- `NewBadRequestError(message string)`
- `NewUnauthorizedError(message string)`
- `NewForbiddenError(message string)`
- `NewNotFoundError(message string)`
- `NewFloodError(message string)`
- `NewInternalError(message string)`
- `FromError(err error) *RPCError`

Runtime behavior:

- Handler/interceptor errors are converted to `RPCError`.
- Error response is serialized as TL object and returned via MTProto encrypted envelope.
