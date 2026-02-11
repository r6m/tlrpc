## Phase 6: Session & Protocol
**Duration**: 3 weeks
**Goal**: Session management and MTProto protocol handling

---

### Task 6.1: Session Management
**Agent**: Session Agent
**Documents**: API.md session section

**Specifications**:
Create `pkg/session/session.go` and `pkg/session/manager.go`.

**Session Structure**:
```go
package session

import (
    "sync"
    "time"
    
    "github.com/yourorg/tlrpc/pkg/crypto"
)

type Session struct {
    ID           int64
    AuthKeyID    crypto.KeyID
    Layer        int
    UserID       int64
    CreatedAt    time.Time
    LastActivity time.Time
    
    // Sequence numbers for message ordering
    SeqNo int32
    
    // Message IDs for deduplication
    RecentMsgIDs *lru.Cache // of int64
    
    // User-defined storage
    Data sync.Map
}

func (s *Session) IsAuthorized() bool
func (s *Session) Touch()
```

**Manager Interface**:
```go
type Manager interface {
    Get(authKeyID crypto.KeyID) (*Session, error)
    Create(authKeyID crypto.KeyID) (*Session, error)
    Save(session *Session) error
    Delete(authKeyID crypto.KeyID) error
    GC(maxAge time.Duration) // Remove inactive sessions
}

// Memory implementation
type MemoryManager struct {
    mu       sync.RWMutex
    sessions map[crypto.KeyID]*Session
}
```

**Deliverables**:
- `pkg/session/session.go` - Session type
- `pkg/session/manager.go` - Manager interface and memory impl
- `pkg/session/manager_test.go` - Tests

**Verification**:
- [ ] Thread-safe operations
- [ ] LRU cache for message IDs works
- [ ] GC removes old sessions

---

### Task 6.2: Message Framing
**Agent**: Protocol Agent
**Documents**: MTProto specification

**Specifications**:
Create `pkg/mtproto/message.go` for MTProto message structure.

**Message Types**:
```go
package mtproto

// UnencryptedMessage for handshake only
type UnencryptedMessage struct {
    AuthKeyID [8]byte // Always 0
    MsgID     int64
    Data      []byte
}

// EncryptedMessage for all RPC
type EncryptedMessage struct {
    AuthKeyID     crypto.KeyID
    MsgKey        [16]byte
    EncryptedData []byte // Contains inner data + padding
}

// InnerData after decryption
type InnerData struct {
    Salt       int64
    SessionID  int64
    MsgID      int64
    SeqNo      int32
    Data       []byte // Serialized TL object
}
```

**Serialization**:
```go
func (m *UnencryptedMessage) Serialize() ([]byte, error)
func (m *UnencryptedMessage) Deserialize([]byte) error
func (m *EncryptedMessage) Decrypt(key crypto.AuthKey) (*InnerData, error)
func (m *InnerData) Encrypt(key crypto.AuthKey, authKeyID crypto.KeyID) (*EncryptedMessage, error)
```

**MsgKey Calculation**:
SHA1(auth_key_fragment + decrypted_data)[:16]

**Deliverables**:
- `pkg/mtproto/message.go` - Message types
- `pkg/mtproto/message_test.go` - Serialization tests

**Verification**:
- [ ] Unencrypted messages serialize correctly
- [ ] Encryption/decryption round-trips
- [ ] MsgKey calculation is correct

---

### Task 6.3: Connection Handler
**Agent**: Protocol Agent
**Documents**: ARCHITECTURE.md data flow

**Specifications**:
Create `pkg/tlrpc/conn.go` handling individual client connections.

**Structure**:
```go
package tlrpc

import (
    "github.com/yourorg/tlrpc/pkg/crypto"
    "github.com/yourorg/tlrpc/pkg/session"
    "github.com/yourorg/tlrpc/pkg/transport"
)

type connHandler struct {
    server *Server
    conn   transport.Conn
    session *session.Session
    authKey crypto.AuthKey
}

func (h *connHandler) run()
func (h *connHandler) handleMessage(data []byte) error
func (h *connHandler) processRPC(data []byte) ([]byte, error)
func (h *connHandler) sendError(err error)
```

**Flow**:
1. Accept connection
2. Perform handshake (get auth key)
3. Create/load session
4. Read encrypted messages
5. Decrypt → get inner data
6. Deserialize TL object (using session.Layer)
7. Route to service
8. Serialize response
9. Encrypt and send

**Deliverables**:
- `pkg/tlrpc/conn.go` - Connection handler
- `pkg/tlrpc/conn_test.go` - Mock-based tests

**Verification**:
- [ ] Handles complete RPC cycle
- [ ] Proper error responses
- [ ] Session persistence across messages

