# Type Domains

TLRPC keeps TL objects split into three domains:

1. Built-in TL primitives in `/Users/reza/Documents/projects/sandbox/tlrpc/types`.
2. MTProto envelope/control TL objects in `/Users/reza/Documents/projects/sandbox/tlrpc/mtproto/tl`.
3. API schema types/methods in generated packages (for example `/Users/reza/Documents/projects/sandbox/tlrpc/gen`).

Rules:

- `conn.go` should not hardcode MTProto constructor IDs.
- MTProto IDs must be represented by typed objects in `mtproto/tl`.
- Generated API code must not include MTProto envelope objects.
