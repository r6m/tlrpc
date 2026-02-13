## Phase 8: Layer Support (Codec-Based)
**Duration**: 2 weeks
**Goal**: Multi-layer client support via codecs and registries

---

### Task 8.1: Codec Registry
**Agent**: Core Agent
**Documents**: codec package design

**Specifications**:
Provide a constructor registry that maps TL constructor IDs and method names to concrete Go types.

**API**:
```go
package codec

type ConstructorFunc func() tlrpc.TLObject

type Registry struct {
    // constructor ID -> new instance
}

func (r *Registry) RegisterConstructor(id uint32, fn ConstructorFunc)
func (r *Registry) RegisterMethod(name string, fn ConstructorFunc)
func (r *Registry) LookupConstructor(id uint32) (ConstructorFunc, bool)
func (r *Registry) LookupMethod(name string) (ConstructorFunc, bool)
```

**Deliverables**:
- `codec/codec.go` registry + codec

**Verification**:
- [ ] Lookup by constructor ID works
- [ ] Unknown constructor returns clear error

---

### Task 8.2: Generated Constructor Registration
**Agent**: CodeGen Agent
**Documents**: tlrpc-gen output

**Specifications**:
Extend codegen to emit a helper to populate a registry with TL constructors and RPC request types.

**Generated Helper**:
```go
func RegisterCodec(reg *codec.Registry) {
    reg.RegisterConstructor(UserConstructorID, func() tlrpc.TLObject { return &User{} })
    reg.RegisterConstructor((&SendCodeRequest{}).ConstructorID(), func() tlrpc.TLObject { return &SendCodeRequest{} })
}
```

**Deliverables**:
- `internal/codegen/gen_codec.go`

**Verification**:
- [ ] Requests can be decoded via codec registry
- [ ] Non-RPC TL objects decode/encode

---

### Task 8.3: Layered Codecs
**Agent**: Core Agent
**Documents**: tlrpc Codec interface

**Specifications**:
Provide a codec implementation that dispatches by `layer` argument to different registries.

**Example**:
```go
type LayeredCodec struct {
    byLayer map[int]*codec.Registry
}

func (c *LayeredCodec) Decode(layer int, data []byte) (tlrpc.TLObject, error)
func (c *LayeredCodec) Encode(layer int, obj tlrpc.TLObject) ([]byte, error)
```

**Deliverables**:
- `codec/layered.go` (optional)

**Verification**:
- [ ] Can register two layers and route correctly
