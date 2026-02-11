## Phase 8: Layer Support
**Duration**: 2 weeks
**Goal**: Multi-layer client support

---

### Task 8.1: Layer Registry
**Agent**: Layer Agent
**Documents**: DESIGN.md layer handling

**Specifications**:
Create `pkg/layer/layer.go` and `pkg/layer/registry.go`.

**Layer Interface**:
```go
package layer

import (
    "io"
    
    "github.com/yourorg/tlrpc/pkg/mtproto"
)

// Layer handles serialization for a specific TL layer version
type Layer interface {
    Version() int
    
    // Deserialize TL object from reader
    // Returns (object, constructorID, error)
    Deserialize(r io.Reader) (mtproto.TLObject, uint32, error)
    
    // Serialize TL object to writer
    Serialize(w io.Writer, obj mtproto.TLObject) error
    
    // Get constructor ID for an object
    GetConstructorID(obj mtproto.TLObject) (uint32, bool)
    
    // Get method name for RPC object
    GetMethodName(obj mtproto.TLObject) (string, bool)
}

// Registry holds all supported layers
type Registry struct {
    mu     sync.RWMutex
    layers map[int]Layer
    max    int
}

func (r *Registry) Register(l Layer)
func (r *Registry) Get(version int) (Layer, bool)
func (r *Registry) Max() int
```

**Generated Layer Implementation**:
Each generated layer package implements this interface.

**Deliverables**:
- `pkg/layer/layer.go` - Interface
- `pkg/layer/registry.go` - Registry
- `pkg/layer/registry_test.go` - Tests

**Verification**:
- [ ] Can register multiple layers
- [ ] Lookup by version works
- [ ] Max layer tracked correctly

---

### Task 8.2: Layer Code Generation
**Agent**: CodeGen Agent
**Documents**: Phase 2 output

**Specifications**:
Extend code generator to create per-layer packages.

**Generated Structure**:
```
gen/
├── layer195/
│   ├── layer.go          # Implements layer.Layer
│   ├── types.go          # All types for layer 195
│   ├── serialize.go      # SerializeTL methods
│   └── deserialize.go    # DeserializeTL methods
├── layer196/
│   └── ...
└── ...
```

**Layer Implementation Template**:
```go
package layer195

import (
    "io"
    
    "github.com/yourorg/tlrpc/pkg/mtproto"
)

type Layer struct{}

func (l *Layer) Version() int { return 195 }

func (l *Layer) Deserialize(r io.Reader) (mtproto.TLObject, uint32, error) {
    // Read constructor ID
    var ctorID uint32
    if err := binary.Read(r, binary.LittleEndian, &ctorID); err != nil {
        return nil, 0, err
    }
    
    // Route to deserializer
    switch ctorID {
    case 0x8f97c628: // user
        obj := &User{}
        return obj, ctorID, obj.DeserializeTL(r)
    // ... other constructors
    default:
        return nil, ctorID, fmt.Errorf("unknown constructor: %x", ctorID)
    }
}

func (l *Layer) Serialize(w io.Writer, obj mtproto.TLObject) error {
    // Type switch to find correct serializer
    switch o := obj.(type) {
    case *User:
        return o.SerializeTL(w)
    // ... other types
    default:
        return fmt.Errorf("unknown type: %T", obj)
    }
}

func (l *Layer) GetConstructorID(obj mtproto.TLObject) (uint32, bool) {
    if t, ok := obj.(interface{ ConstructorID() uint32 }); ok {
        return t.ConstructorID(), true
    }
    return 0, false
}

func (l *Layer) GetMethodName(obj mtproto.TLObject) (string, bool) {
    if t, ok := obj.(interface{ Method() string }); ok {
        return t.Method(), true
    }
    return "", false
}
```

**Deliverables**:
- Update `pkg/codegen/` to generate layer packages
- `pkg/codegen/gen_layer.go` - Layer generator

**Verification**:
- [ ] Generated layers implement interface
- [ ] Can deserialize all types in layer
- [ ] Constructor ID routing is correct