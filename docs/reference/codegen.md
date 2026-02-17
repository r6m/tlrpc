# Code Generation

CLI:

```bash
tlrpc-gen --schema=./schema.tl --out=./gen --package=gen
```

Generated files include:

- `types.go`: generated TL type structs/interfaces.
- `interfaces.go`: union interfaces.
- `services.go`: service interfaces + unimplemented stubs.
- `register.go`: `Register*Server` + `ServiceDesc` values.
- `requests.go`: RPC request structs + serialization.
- `codec.go`: static constructor/method maps.
- `constants.go`: constructor ID constants.
- `base_aliases.go`: aliases for built-in TL primitives from `github.com/r6m/tlrpc/types`.

Service model:

- Generated requests expose `Method() string`.
- Generated registration descriptors provide constructor ID and request constructors used by runtime dispatch.
- Built-in TL primitives are referenced via aliases instead of re-generated shadow types.

If you change schema/signatures, regenerate and recompile consumers.
