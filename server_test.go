package tlrpc

import (
	"context"
	"testing"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/session"
)

// mockLogger implements Logger interface for testing
type mockLogger struct {
	infoMsgs  []string
	errorMsgs []string
	debugMsgs []string
}

func (m *mockLogger) Info(msg string, args ...interface{}) {
	m.infoMsgs = append(m.infoMsgs, msg)
}

func (m *mockLogger) Error(msg string, args ...interface{}) {
	m.errorMsgs = append(m.errorMsgs, msg)
}

func (m *mockLogger) Debug(msg string, args ...interface{}) {
	m.debugMsgs = append(m.debugMsgs, msg)
}

func TestNewServer(t *testing.T) {
	s := NewServer()

	if s == nil {
		t.Fatal("NewServer returned nil")
	}

	// Check default initialization
	if s.dispatcher == nil {
		t.Error("dispatcher not initialized")
	}
	if s.authKeys == nil {
		t.Error("authKeys not initialized")
	}
	if s.serverKeys == nil {
		t.Error("serverKeys not initialized")
	}
	if s.sessions == nil {
		t.Error("sessions not initialized")
	}
	if s.services == nil {
		t.Error("services map not initialized")
	}
	if s.shutdownCh == nil {
		t.Error("shutdownCh not initialized")
	}
}

func TestServerOptions(t *testing.T) {
	// Test WithMaxLayer
	s := NewServer(WithMaxLayer(42))
	if s.maxLayer != 42 {
		t.Error("WithMaxLayer option not applied")
	}

	// Test WithLayers
	layers := []int{1, 2, 3}
	s = NewServer(WithLayers(layers...))
	if len(s.layers) != 3 || s.layers[0] != 1 || s.layers[1] != 2 || s.layers[2] != 3 {
		t.Error("WithLayers option not applied correctly")
	}

	// Test WithLogger
	mockLog := &mockLogger{}
	s = NewServer(WithLogger(mockLog))
	if s.logger != mockLog {
		t.Error("WithLogger option not applied")
	}

	// Test WithAuthKeyManager
	mockAuthKeys := crypto.NewMemoryAuthKeyManager()
	s = NewServer(WithAuthKeyManager(mockAuthKeys))
	if s.authKeys != mockAuthKeys {
		t.Error("WithAuthKeyManager option not applied")
	}

	// Test WithServerKeyManager
	mockServerKeys := crypto.NewMemoryServerKeyManager()
	s = NewServer(WithServerKeyManager(mockServerKeys))
	if s.serverKeys != mockServerKeys {
		t.Error("WithServerKeyManager option not applied")
	}

	// Test WithSessionManager
	mockSessions := session.NewMemoryManager()
	s = NewServer(WithSessionManager(mockSessions))
	if s.sessions != mockSessions {
		t.Error("WithSessionManager option not applied")
	}
}

func TestRegisterService(t *testing.T) {
	s := NewServer()

	// Test successful registration
	desc := ServiceDesc{
		ServiceName: "TestService",
		Methods:     []MethodDesc{},
	}
	impl := struct{}{}

	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = r.(error)
			}
		}()
		s.RegisterService(desc, impl)
		return nil
	}()

	if err != nil {
		t.Fatalf("RegisterService panicked: %v", err)
	}

	// Verify service was registered
	if s.services["TestService"] == nil {
		t.Error("service not registered")
	}
	if s.services["TestService"].desc.ServiceName != "TestService" {
		t.Error("service desc not stored correctly")
	}
	if s.services["TestService"].impl != impl {
		t.Error("service impl not stored correctly")
	}

	// Test duplicate registration (should panic)
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate service registration")
		}
	}()
	s.RegisterService(desc, impl)
}

func TestRegisterServiceValidation(t *testing.T) {
	s := NewServer()

	// Test empty service name (should panic)
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on empty service name")
		}
	}()
	desc := ServiceDesc{ServiceName: ""}
	s.RegisterService(desc, struct{}{})

	// Test nil implementation (should panic)
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil implementation")
		}
	}()
	desc = ServiceDesc{ServiceName: "TestService"}
	s.RegisterService(desc, nil)
}

func TestServerStop(t *testing.T) {
	s := NewServer()

	// Test Stop doesn't panic
	err := s.Stop()
	if err != nil {
		t.Errorf("Stop returned error: %v", err)
	}

	// Test multiple stops are safe
	err = s.Stop()
	if err != nil {
		t.Errorf("Second Stop returned error: %v", err)
	}
}

func TestWithUnaryInterceptor(t *testing.T) {
	interceptor := func(ctx context.Context, req interface{}, info *UnaryServerInfo, handler UnaryHandler) (interface{}, error) {
		return handler(ctx, req)
	}

	s := NewServer(WithUnaryInterceptor(interceptor))

	if len(s.unaryInterceptors) != 1 {
		t.Errorf("expected 1 interceptor, got %d", len(s.unaryInterceptors))
	}

	// Test that interceptor is stored
	if s.unaryInterceptors[0] == nil {
		t.Error("interceptor not stored")
	}
}

// TestWithMaxMessageSize is a placeholder test since the implementation is TODO
func TestWithMaxMessageSize(t *testing.T) {
	s := NewServer(WithMaxMessageSize(1024))
	// Currently does nothing, but ensures no panic
	if s == nil {
		t.Error("server is nil")
	}
}

// TestWithMaxConcurrentStreams is a placeholder test since the implementation is TODO
func TestWithMaxConcurrentStreams(t *testing.T) {
	s := NewServer(WithMaxConcurrentStreams(10))
	// Currently does nothing, but ensures no panic
	if s == nil {
		t.Error("server is nil")
	}
}

// TestWithReadTimeout is a placeholder test since the implementation is TODO
func TestWithReadTimeout(t *testing.T) {
	s := NewServer(WithReadTimeout(time.Second))
	// Currently does nothing, but ensures no panic
	if s == nil {
		t.Error("server is nil")
	}
}

// TestWithWriteTimeout is a placeholder test since the implementation is TODO
func TestWithWriteTimeout(t *testing.T) {
	s := NewServer(WithWriteTimeout(time.Second))
	// Currently does nothing, but ensures no panic
	if s == nil {
		t.Error("server is nil")
	}
}
