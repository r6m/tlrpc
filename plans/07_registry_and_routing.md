## Phase 7: Service Registry & Routing
**Duration**: 2 weeks
**Goal**: Route requests to service implementations

---

### Task 7.1: Service Registry
**Agent**: Registry Agent
**Documents**: API.md service registration

**Specifications**:
Create `pkg/tlrpc/registry.go`.

**Structure**:
```go
package tlrpc

import (
    "context"
    "fmt"
    "sync"
)

type serviceRegistry struct {
    mu       sync.RWMutex
    services map[string]*serviceInfo // by service name
    methods  map[string]MethodDesc   // by full method name (e.g., "auth.sendCode")
}

type serviceInfo struct {
    desc ServiceDesc
    impl interface{}
}

func (r *serviceRegistry) register(desc ServiceDesc, impl interface{}) error
func (r *serviceRegistry) getMethod(name string) (MethodDesc, bool)
func (r *serviceRegistry) listServices() []string
```

**Registration Validation**:
- Check for duplicate method names
- Verify implementation matches interface (via reflection or code generation)

**Deliverables**:
- `pkg/tlrpc/registry.go` - Registry implementation
- `pkg/tlrpc/registry_test.go` - Registration tests

**Verification**:
- [ ] Thread-safe registration
- [ ] Duplicate detection works
- [ ] Method lookup is O(1)

---

### Task 7.2: Interceptor Chain
**Agent**: Middleware Agent
**Documents**: API.md interceptors

**Specifications**:
Create `pkg/tlrpc/interceptor.go`.

**Chain Builder**:
```go
package tlrpc

type Interceptor func(next Handler) Handler

func ChainInterceptors(interceptors ...Interceptor) Interceptor {
    return func(final Handler) Handler {
        for i := len(interceptors) - 1; i >= 0; i-- {
            final = interceptors[i](final)
        }
        return final
    }
}

// Built-in interceptors
func RecoveryInterceptor() Interceptor
func LoggingInterceptor(logger Logger) Interceptor
func AuthInterceptor(authorizer Authorizer) Interceptor
```

**Context Values**:
```go
func SessionFromContext(ctx context.Context) *session.Session
func LayerFromContext(ctx context.Context) int
func AuthKeyIDFromContext(ctx context.Context) crypto.KeyID
```

**Deliverables**:
- `pkg/tlrpc/interceptor.go` - Chain and built-ins
- `pkg/tlrpc/interceptor_test.go` - Chain tests

**Verification**:
- [ ] Interceptors execute in correct order
- [ ] Context values propagate
- [ ] Recovery catches panics

