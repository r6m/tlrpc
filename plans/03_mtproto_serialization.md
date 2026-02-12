## Phase 3: MTProto Serialization
**Duration**: 3 weeks
**Goal**: Binary serialization/deserialization for generated types

---

### Task 3.1: Primitive Serializers
**Agent**: Serialization Agent
**Documents**: ARCHITECTURE.md serialization section

**Specifications**:
Create `mtproto/serialize.go` for primitive type serialization.

**Functions**:
```go
package mtproto

import "io"

// Write int32 (little-endian)
func WriteInt32(w io.Writer, v int32) error

// Write int64 (little-endian)
func WriteInt64(w io.Writer, v int64) error

// Write int128 ([16]byte)
func WriteInt128(w io.Writer, v [16]byte) error

// Write int256 ([32]byte)
func WriteInt256(w io.Writer, v [32]byte) error

// Write double (float64, IEEE 754 LE)
func WriteDouble(w io.Writer, v float64) error

// Write string (length as varint + bytes)
func WriteString(w io.Writer, v string) error

// Write bytes (length as varint + bytes)
func WriteBytes(w io.Writer, v []byte) error

// Write Bool (0x997275b5 for true, 0xbc799737 for false)
func WriteBool(w io.Writer, v bool) error

// Write vector header (count as int32)
func WriteVectorHeader(w io.Writer, count int) error
```

**Buffer Pool**:
Use `sync.Pool` for `*bytes.Buffer` to reduce allocations.

**Deliverables**:
- `mtproto/serialize.go` - Writers
- `mtproto/serialize_test.go` - Round-trip tests
- `mtproto/buffer.go` - Buffer pool

**Verification**:
- [ ] All primitives round-trip correctly
- [ ] Byte order is little-endian
- [ ] String encoding matches Telegram spec
- [ ] Benchmark: <50ns per int32 write

---

### Task 3.2: Primitive Deserializers
**Agent**: Serialization Agent
**Documents**: ARCHITECTURE.md deserialization section

**Specifications**:
Create `mtproto/deserialize.go` for primitive type deserialization.

**Functions**:
```go
package mtproto

import "io"

func ReadInt32(r io.Reader) (int32, error)
func ReadInt64(r io.Reader) (int64, error)
func ReadInt128(r io.Reader) ([16]byte, error)
func ReadInt256(r io.Reader) ([32]byte, error)
func ReadDouble(r io.Reader) (float64, error)
func ReadString(r io.Reader) (string, error)
func ReadBytes(r io.Reader) ([]byte, error)
func ReadBool(r io.Reader) (bool, error)

// Read vector, call fn for each element
func ReadVector(r io.Reader, fn func() error) error
```

**Error Handling**:
- `io.ErrUnexpectedEOF` on short reads
- `ErrInvalidBool` for invalid bool values
- `ErrStringTooLong` for strings > 2^31 bytes

**Deliverables**:
- `mtproto/deserialize.go` - Readers
- `mtproto/deserialize_test.go` - Round-trip tests

**Verification**:
- [ ] Perfect round-trip with serializers
- [ ] Handles EOF gracefully
- [ ] Validates bool values

---

### Task 3.3: Generated Serialization Methods
**Agent**: CodeGen Agent
**Documents**: Phase 2.2 type mapping

**Specifications**:
Extend type generator to produce `SerializeTL` and `DeserializeTL` methods.

**Generated Example**:
```go
func (u *User) SerializeTL(w io.Writer) error {
    // Write constructor ID
    if err := WriteUint32(w, u.ConstructorID()); err != nil {
        return err
    }
    
    // Compute and write flags
    flags := u.computeFlags()
    if err := WriteUint32(w, flags); err != nil {
        return err
    }
    
    // Write ID (always present)
    if err := WriteInt64(w, u.ID); err != nil {
        return err
    }
    
    // Write FirstName if flag set
    if flags & (1 << 0) != 0 {
        if err := WriteString(w, *u.FirstName); err != nil {
            return err
        }
    }
    
    return nil
}

func (u *User) computeFlags() uint32 {
    var flags uint32
    if u.FirstName != nil {
        flags |= 1 << 0
    }
    // ... other flags
    return flags
}

func (u *User) DeserializeTL(r io.Reader) error {
    // Read constructor ID (already read by caller, verify)
    var ctorID uint32
    if err := ReadUint32(r, &ctorID); err != nil {
        return err
    }
    if ctorID != u.ConstructorID() {
        return fmt.Errorf("wrong constructor: got %x, want %x", ctorID, u.ConstructorID())
    }
    
    // Read flags
    var flags uint32
    if err := ReadUint32(r, &flags); err != nil {
        return err
    }
    
    // Read ID
    if err := ReadInt64(r, &u.ID); err != nil {
        return err
    }
    
    // Conditionally read FirstName
    if flags & (1 << 0) != 0 {
        s, err := ReadString(r)
        if err != nil {
            return err
        }
        u.FirstName = &s
    }
    
    return nil
}
```

**Deliverables**:
- Update `internal/codegen/gen_types.go` with serialization methods
- `internal/codegen/gen_serialize_test.go` - Generated code tests

**Verification**:
- [ ] Generated serialization matches Telegram format
- [ ] Round-trip: serialize → deserialize → equal
- [ ] Handles all flag combinations
- [ ] Polymorphic types serialize correctly

