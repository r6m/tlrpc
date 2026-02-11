## Phase 5: Cryptography
**Duration**: 4 weeks
**Goal**: MTProto cryptographic primitives

---

### Task 5.1: AES-256-IGE
**Agent**: Crypto Agent
**Documents**: MTProto specification (external)

**Specifications**:
Create `pkg/crypto/aes.go` implementing AES-256-IGE mode.

**IGE Mode**:
- Infinite Garble Extension
- Required for MTProto message encryption
- Not in standard library

**Interface**:
```go
package crypto

import "crypto/cipher"

// NewAESIGE creates AES-256-IGE cipher
func NewAESIGE(key, iv []byte) cipher.BlockMode

// Encrypt/decrypt using IGE mode
func EncryptIGE(dst, src []byte, block cipher.Block, iv []byte)
func DecryptIGE(dst, src []byte, block cipher.Block, iv []byte)
```

**Properties**:
- Block size: 16 bytes
- Key size: 32 bytes (AES-256)
- IV size: 32 bytes (16 for x, 16 for y)

**Deliverables**:
- `pkg/crypto/aes.go` - IGE implementation
- `pkg/crypto/aes_test.go` - Test vectors from MTProto spec
- `pkg/crypto/aes_bench_test.go` - Performance benchmarks

**Verification**:
- [ ] Matches test vectors from Telegram docs
- [ ] No data-dependent timing (constant time)
- [ ] Benchmark: >500MB/s on modern CPU

---

### Task 5.2: Auth Key Management
**Agent**: Crypto Agent
**Documents**: ARCHITECTURE.md crypto section

**Specifications**:
Create `pkg/crypto/authkey.go` for auth key handling.

**Types**:
```go
package crypto

import "encoding/binary"

// AuthKey is a 256-bit shared secret
type AuthKey [32]byte

// KeyID is the first 64 bits of SHA1(auth_key)
type KeyID uint64

func (k AuthKey) ID() KeyID {
    hash := sha1.Sum(k[:])
    return KeyID(binary.LittleEndian.Uint64(hash[:8]))
}

// AuthKeyManager stores and retrieves keys
type AuthKeyManager interface {
    Get(keyID KeyID) (AuthKey, error)
    Put(keyID KeyID, key AuthKey) error
    Delete(keyID KeyID) error
}
```

**Implementation**:
- In-memory implementation for testing
- Interface for pluggable storage

**Deliverables**:
- `pkg/crypto/authkey.go` - Auth key types
- `pkg/crypto/authkey_memory.go` - In-memory store
- `pkg/crypto/authkey_test.go` - Tests

**Verification**:
- [ ] KeyID calculation matches Telegram
- [ ] Thread-safe operations
- [ ] Constant-time comparison

---

### Task 5.3: MTProto Handshake
**Agent**: Crypto Agent
**Documents**: MTProto specification (external)

**Specifications**:
Create `pkg/crypto/handshake.go` for initial key exchange.

**Handshake Steps**:
1. Client sends `req_pq_multi` (unencrypted)
2. Server responds with `server_DH_params_ok`
3. Client sends `set_client_DH_params`
4. Auth key established

**Simplified Interface** (for framework):
```go
type Handshake struct {
    rng io.Reader
}

func (h *Handshake) Process(req []byte) (resp []byte, key AuthKey, err error)
```

**Note**: Full handshake is complex; implement simplified version for framework, document that production needs full implementation.

**Deliverables**:
- `pkg/crypto/handshake.go` - Handshake logic
- `pkg/crypto/handshake_test.go` - Test with known vectors

**Verification**:
- [ ] Generates valid auth keys
- [ ] Handles all handshake steps
- [ ] Rejects invalid requests

