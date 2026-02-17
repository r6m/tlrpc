# TL Schema

TLRPC consumes Telegram TL schema files with two sections:

- `---types---`: constructor declarations for data/object variants.
- `---functions---`: RPC method declarations.

Both sections use the same syntax:

```tl
name#<hex-id> <fields...> = <result-type>;
```

Key points:

- Constructor IDs identify concrete object variants on the wire.
- Method IDs (function constructor IDs) identify RPC requests.
- The parser keeps these separate as `schema.Constructors` and `schema.Functions`.
- Generated request structs implement `ConstructorID() uint32` and `Method() string`.
- Dispatch in `conn.go` routes by method constructor ID.

Built-in TL primitives (`int`, `long`, `string`, `bytes`, `Bool`, `Vector<T>`) are handled by codec/runtime support, not service handlers.
