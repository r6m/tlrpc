## Phase 4: Transport Layer
**Duration**: 3 weeks
**Goal**: Network transport implementations

---

### Task 4.1: Transport Interface
**Agent**: Network Agent
**Documents**: API.md transport section

**Specifications**:
Create `transport/transport.go` defining transport abstractions.

**Interfaces**:
```go
package transport

import (
    "context"
    "net"
    "time"
)

// Transport creates listeners and connections
type Transport interface {
    Listen(addr string) (Listener, error)
    Dial(addr string) (Conn, error)
}

// Listener accepts connections
type Listener interface {
    Accept() (Conn, error)
    Close() error
    Addr() net.Addr
}

// Conn is a transport connection
type Conn interface {
    // ReadMessage reads a complete message (MTProto frame)
    ReadMessage() ([]byte, error)
    
    // WriteMessage writes a complete message
    WriteMessage([]byte) error
    
    // Close closes the connection
    Close() error
    
    // LocalAddr returns local address
    LocalAddr() net.Addr
    
    // RemoteAddr returns remote address
    RemoteAddr() net.Addr
    
    // SetDeadline sets read/write deadlines
    SetDeadline(t time.Time) error
    
    // Context returns connection context (cancelled on close)
    Context() context.Context
}
```

**Message Framing**:
MTProto uses length-prefixed frames:
```
4 bytes: length (little-endian, includes length itself)
N bytes: payload
0-3 bytes: padding to 4-byte boundary
```

**Deliverables**:
- `transport/transport.go` - Core interfaces
- `transport/common.go` - Shared framing logic
- `transport/transport_test.go` - Interface tests

**Verification**:
- [ ] Interfaces are implementable
- [ ] Framing handles all message sizes
- [ ] Padding is correct

---

### Task 4.2: TCP Transport
**Agent**: Network Agent
**Documents**: ARCHITECTURE.md transport section

**Specifications**:
Create `transport/tcp.go` implementing TCP transport.

**Features**:
- Standard TCP sockets
- Keepalive settings
- NoDelay (disable Nagle)
- Connection timeouts
- Graceful shutdown

**Implementation**:
```go
type TCPTransport struct {
    ReadTimeout  time.Duration
    WriteTimeout time.Duration
}

func (t *TCPTransport) Listen(addr string) (Listener, error)
func (t *TCPTransport) Dial(addr string) (Conn, error)

type tcpConn struct {
    net.Conn
    r *bufio.Reader
    w *bufio.Writer
}
```

**Deliverables**:
- `transport/tcp.go` - TCP implementation
- `transport/tcp_test.go` - Unit and integration tests

**Verification**:
- [ ] Passes transport interface tests
- [ ] Handles 10k concurrent connections
- [ ] Graceful shutdown closes all connections
- [ ] Benchmark: >1Gbps throughput

---

### Task 4.3: WebSocket Transport
**Agent**: Network Agent
**Documents**: ARCHITECTURE.md transport section

**Specifications**:
Create `transport/websocket.go` for WebSocket clients.

**Features**:
- Binary message mode only
- Subprotocol: `binary`
- Ping/pong handling
- Close handshake

**Implementation**:
```go
type WebSocketTransport struct {
    Upgrader websocket.Upgrader
}

func (t *WebSocketTransport) Listen(addr string) (Listener, error)
func (t *WebSocketTransport) Dial(addr string) (Conn, error)

type wsConn struct {
    conn *websocket.Conn
    // ...
}
```

**Deliverables**:
- `transport/websocket.go` - WebSocket implementation
- `transport/websocket_test.go` - Tests with gorilla/websocket

**Verification**:
- [ ] Works with browser clients
- [ ] Handles connection drops
- [ ] Binary framing correct

