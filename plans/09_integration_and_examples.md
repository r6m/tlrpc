## Phase 9: Integration & Examples
**Duration**: 2 weeks
**Goal**: Working examples and integration tests

---

### Task 9.1: Echo Example
**Agent**: Example Agent
**Documents**: examples/echo/ specification

**Specifications**:
Create `examples/echo/` - minimal working server.

**Files**:
```
examples/echo/
├── echo.tl              # Simple TL schema for echo
├── server.go            # Server implementation
├── client.go            # Test client
└── README.md            # Instructions
```

**echo.tl**:
```
---types---
echoRequest#12345678 message:string = EchoRequest;
echoResponse#87654321 message:string = EchoResponse;

---functions---
echo.send#abcdef01 req:EchoRequest = EchoResponse;
```

**server.go**:
```go
package main

import (
    "context"
    "log"
    "net"
    
    "github.com/r6m/tlrpc/pkg/tlrpc"
    "github.com/r6m/tlrpc/examples/echo/gen"
)

type EchoService struct {
    gen.UnimplementedEchoServer
}

func (s *EchoService) Send(ctx context.Context, req *gen.EchoRequest) (*gen.EchoResponse, error) {
    return &gen.EchoResponse{Message: "Echo: " + req.Message}, nil
}

func main() {
    server := tlrpc.NewServer()
    gen.RegisterEchoServer(server, &EchoService{})
    
    lis, err := net.Listen("tcp", ":8080")
    if err != nil {
        log.Fatal(err)
    }
    
    log.Println("Echo server on :8080")
    server.Serve(lis)
}
```

**Deliverables**:
- Complete echo example
- `make run-example` target in main Makefile

**Verification**:
- [ ] `go generate` creates gen/ directory
- [ ] Server starts and accepts connections
- [ ] Client can send echo request and receive response

---

### Task 9.2: Integration Test Suite
**Agent**: QA Agent
**Documents**: Test patterns in CONTRIBUTING.md

**Specifications**:
Create `tests/integration/` with full integration tests.

**Test Scenarios**:
1. **Basic Connectivity**: Client connects, handshake succeeds
2. **RPC Round-trip**: Request → Response with correct data
3. **Multi-layer**: Layer 195 and Layer 222 clients work
4. **Error Handling**: Invalid requests return proper errors
5. **Concurrency**: 1000 concurrent clients
6. **Reconnection**: Client reconnects, session restored

**Framework**:
```go
package integration

import (
    "testing"
    
    "github.com/r6m/tlrpc/pkg/tlrpc"
    "github.com/r6m/tlrpc/internal/testutil"
)

func TestBasicConnectivity(t *testing.T) {
    // Setup server
    server := tlrpc.NewServer()
    // ... register services
    
    lis := testutil.NewLocalListener()
    go server.Serve(lis)
    defer server.Stop()
    
    // Connect client
    client := testutil.NewTestClient(lis.Addr().String())
    err := client.Connect()
    require.NoError(t, err)
    
    // Send ping
    err = client.Ping()
    require.NoError(t, err)
}
```

**Deliverables**:
- `tests/integration/basic_test.go`
- `tests/integration/concurrency_test.go`
- `tests/integration/layer_test.go`
- `internal/testutil/client.go` - Test client

**Verification**:
- [ ] All tests pass
- [ ] Tests run in CI
- [ ] Coverage >80%

