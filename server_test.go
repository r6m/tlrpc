package tlrpc

import (
	"context"
	"fmt"
	"strings"
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

type testServiceRequest struct {
	constructorID uint32
}

func (r *testServiceRequest) ConstructorID() uint32 { return r.constructorID }

type testServiceServer interface {
	Call(context.Context, *testServiceRequest) (*testServiceRequest, error)
}

type testServiceImplementation struct{}

func (testServiceImplementation) Call(_ context.Context, req *testServiceRequest) (*testServiceRequest, error) {
	return req, nil
}

func testServiceCallHandler(srv interface{}, ctx context.Context, req *testServiceRequest) (*testServiceRequest, error) {
	return srv.(testServiceServer).Call(ctx, req)
}

func completeServiceDesc(name string, layer int, constructorID uint32) ServiceDesc {
	return ServiceDesc{
		ServiceName: name,
		SchemaLayer: layer,
		HandlerType: (*testServiceServer)(nil),
		Methods: []MethodDesc{
			{
				MethodName:    "Call",
				ConstructorID: constructorID,
				NewRequest: func() TLObject {
					return &testServiceRequest{constructorID: constructorID}
				},
				Handler: testServiceCallHandler,
			},
		},
	}
}

func requirePanicContains(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("expected panic containing %q", want)
		}
		if got := fmt.Sprint(recovered); !strings.Contains(got, want) {
			t.Fatalf("panic = %q, want substring %q", got, want)
		}
	}()
	fn()
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
	if s.store == nil {
		t.Error("session store not initialized")
	}
	if s.services == nil {
		t.Error("services map not initialized")
	}
	if s.shutdownCh == nil {
		t.Error("shutdownCh not initialized")
	}
}

func TestServerOptions(t *testing.T) {
	// Test WithLogger
	mockLog := &mockLogger{}
	s := NewServer(WithLogger(mockLog))
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

	// Test WithSessionStore
	mockSessions := session.NewMemoryStore()
	s = NewServer(WithSessionStore(mockSessions))
	if s.store != mockSessions {
		t.Error("WithSessionStore option not applied")
	}
}

func TestRegisterService(t *testing.T) {
	s := NewServer()

	desc := completeServiceDesc("TestService", 228, 0xf0010001)
	impl := testServiceImplementation{}
	s.RegisterService(desc, impl)

	if s.services["TestService"] == nil {
		t.Error("service not registered")
	}
	if s.services["TestService"].desc.ServiceName != "TestService" {
		t.Error("service desc not stored correctly")
	}
	if s.services["TestService"].impl != impl {
		t.Error("service impl not stored correctly")
	}
	if s.schemaLayer != 228 {
		t.Fatalf("server schema layer = %d, want 228", s.schemaLayer)
	}
	if _, ok := s.dispatcher.LookupConstructor(0xf0010001); !ok {
		t.Fatal("request constructor not registered")
	}
	if _, ok := s.dispatcher.LookupMethod(0xf0010001); !ok {
		t.Fatal("method handler not registered")
	}

	requirePanicContains(t, "already registered", func() {
		s.RegisterService(desc, impl)
	})
}

func TestRegisterServiceValidation(t *testing.T) {
	const constructorID = uint32(0xf0010010)
	tests := []struct {
		name   string
		want   string
		mutate func(*ServiceDesc) interface{}
	}{
		{name: "service name", want: "service name is required", mutate: func(desc *ServiceDesc) interface{} { desc.ServiceName = ""; return testServiceImplementation{} }},
		{name: "handler type", want: "handler type is required", mutate: func(desc *ServiceDesc) interface{} { desc.HandlerType = nil; return testServiceImplementation{} }},
		{name: "methods", want: "must declare at least one method", mutate: func(desc *ServiceDesc) interface{} { desc.Methods = nil; return testServiceImplementation{} }},
		{name: "implementation", want: "implementation cannot be nil", mutate: func(_ *ServiceDesc) interface{} { return nil }},
		{name: "handler type shape", want: "pointer to an interface", mutate: func(desc *ServiceDesc) interface{} {
			desc.HandlerType = (*testServiceImplementation)(nil)
			return testServiceImplementation{}
		}},
		{name: "implementation contract", want: "does not satisfy handler type", mutate: func(_ *ServiceDesc) interface{} { return struct{}{} }},
		{name: "method name", want: "method with no name", mutate: func(desc *ServiceDesc) interface{} {
			desc.Methods[0].MethodName = ""
			return testServiceImplementation{}
		}},
		{name: "constructor ID", want: "missing constructor ID", mutate: func(desc *ServiceDesc) interface{} {
			desc.Methods[0].ConstructorID = 0
			return testServiceImplementation{}
		}},
		{name: "request constructor", want: "missing request constructor", mutate: func(desc *ServiceDesc) interface{} {
			desc.Methods[0].NewRequest = nil
			return testServiceImplementation{}
		}},
		{name: "nil request", want: "request constructor returned nil", mutate: func(desc *ServiceDesc) interface{} {
			desc.Methods[0].NewRequest = func() TLObject { return nil }
			return testServiceImplementation{}
		}},
		{name: "request ID", want: "does not match descriptor ID", mutate: func(desc *ServiceDesc) interface{} {
			desc.Methods[0].NewRequest = func() TLObject { return &testServiceRequest{constructorID: constructorID + 1} }
			return testServiceImplementation{}
		}},
		{name: "handler", want: "missing handler", mutate: func(desc *ServiceDesc) interface{} { desc.Methods[0].Handler = nil; return testServiceImplementation{} }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			desc := completeServiceDesc("TestService", 228, constructorID)
			impl := test.mutate(&desc)
			s := NewServer()
			requirePanicContains(t, test.want, func() {
				s.RegisterService(desc, impl)
			})
			if s.schemaLayerSet || s.schemaLayer != 0 || len(s.services) != 0 {
				t.Fatal("invalid descriptor mutated server registration state")
			}
		})
	}
}

func TestRegisterServiceEnforcesOneSchemaLayer(t *testing.T) {
	s := NewServer()
	s.RegisterService(completeServiceDesc("First", 228, 0xf0010020), testServiceImplementation{})
	s.RegisterService(completeServiceDesc("Second", 228, 0xf0010021), testServiceImplementation{})

	requirePanicContains(t, "conflicts with server schema layer 228", func() {
		s.RegisterService(completeServiceDesc("OtherLayer", 229, 0xf0010022), testServiceImplementation{})
	})
	if s.schemaLayer != 228 {
		t.Fatalf("server schema layer = %d, want 228", s.schemaLayer)
	}
	if _, exists := s.services["OtherLayer"]; exists {
		t.Fatal("conflicting service was registered")
	}
}

func TestRegisterServiceAcceptsUnlayeredSchemaAndRejectsMixingLayers(t *testing.T) {
	s := NewServer()
	s.RegisterService(completeServiceDesc("Unlayered", 0, 0xf0010023), testServiceImplementation{})
	if !s.schemaLayerSet || s.schemaLayer != 0 {
		t.Fatalf("unlayered schema state = set:%v layer:%d", s.schemaLayerSet, s.schemaLayer)
	}
	requirePanicContains(t, "conflicts with server schema layer 0", func() {
		s.RegisterService(completeServiceDesc("Layered", 228, 0xf0010024), testServiceImplementation{})
	})
}

func TestRegisterServiceRejectsDescriptorAtomically(t *testing.T) {
	const firstConstructorID = uint32(0xf0010030)
	desc := completeServiceDesc("Atomic", 228, firstConstructorID)
	desc.Methods = append(desc.Methods, MethodDesc{
		MethodName:    "Incomplete",
		ConstructorID: 0xf0010031,
		Handler:       testServiceCallHandler,
	})

	s := NewServer()
	requirePanicContains(t, "missing request constructor", func() {
		s.RegisterService(desc, testServiceImplementation{})
	})
	if s.schemaLayer != 0 || len(s.services) != 0 {
		t.Fatal("invalid descriptor mutated service or layer state")
	}
	if _, exists := s.dispatcher.LookupConstructor(firstConstructorID); exists {
		t.Fatal("invalid descriptor partially registered its first constructor")
	}
	if _, exists := s.dispatcher.LookupMethod(firstConstructorID); exists {
		t.Fatal("invalid descriptor partially registered its first method")
	}
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

func TestWithResourceLimits(t *testing.T) {
	s := NewServer(WithResourceLimits(ResourceLimits{
		MaxPayloadBytes:     1024,
		MaxInFlightRequests: 10,
		ReadTimeout:         time.Second,
		WriteTimeout:        2 * time.Second,
	}))
	if s.maxPayloadBytes != 1024 {
		t.Fatalf("max payload bytes = %d, want 1024", s.maxPayloadBytes)
	}
	if cap(s.handlerSlots) != 10 {
		t.Fatalf("handler capacity = %d, want 10", cap(s.handlerSlots))
	}
	if s.readTimeout != time.Second {
		t.Fatalf("read timeout = %v, want 1s", s.readTimeout)
	}
	if s.writeTimeout != 2*time.Second {
		t.Fatalf("write timeout = %v, want 2s", s.writeTimeout)
	}
}

func TestNewServerUsesBoundedResourceDefaults(t *testing.T) {
	s := NewServer()
	if s.maxPayloadBytes != DefaultMaxPayloadBytes {
		t.Fatalf("max payload bytes = %d, want %d", s.maxPayloadBytes, DefaultMaxPayloadBytes)
	}
	if cap(s.handlerSlots) != DefaultMaxInFlightRequests {
		t.Fatalf("handler capacity = %d, want %d", cap(s.handlerSlots), DefaultMaxInFlightRequests)
	}
	if s.readTimeout != DefaultReadTimeout {
		t.Fatalf("read timeout = %v, want %v", s.readTimeout, DefaultReadTimeout)
	}
	if s.writeTimeout != DefaultWriteTimeout {
		t.Fatalf("write timeout = %v, want %v", s.writeTimeout, DefaultWriteTimeout)
	}
}

func TestResourceLimitsRejectInvalidValues(t *testing.T) {
	valid := ResourceLimits{MaxPayloadBytes: 1024, MaxInFlightRequests: 10, ReadTimeout: time.Second, WriteTimeout: time.Second}
	tests := map[string]ResourceLimits{
		"payload zero":           {MaxInFlightRequests: valid.MaxInFlightRequests, ReadTimeout: valid.ReadTimeout, WriteTimeout: valid.WriteTimeout},
		"payload negative":       {MaxPayloadBytes: -1, MaxInFlightRequests: valid.MaxInFlightRequests, ReadTimeout: valid.ReadTimeout, WriteTimeout: valid.WriteTimeout},
		"requests zero":          {MaxPayloadBytes: valid.MaxPayloadBytes, ReadTimeout: valid.ReadTimeout, WriteTimeout: valid.WriteTimeout},
		"requests negative":      {MaxPayloadBytes: valid.MaxPayloadBytes, MaxInFlightRequests: -1, ReadTimeout: valid.ReadTimeout, WriteTimeout: valid.WriteTimeout},
		"read timeout zero":      {MaxPayloadBytes: valid.MaxPayloadBytes, MaxInFlightRequests: valid.MaxInFlightRequests, WriteTimeout: valid.WriteTimeout},
		"read timeout negative":  {MaxPayloadBytes: valid.MaxPayloadBytes, MaxInFlightRequests: valid.MaxInFlightRequests, ReadTimeout: -time.Second, WriteTimeout: valid.WriteTimeout},
		"write timeout zero":     {MaxPayloadBytes: valid.MaxPayloadBytes, MaxInFlightRequests: valid.MaxInFlightRequests, ReadTimeout: valid.ReadTimeout},
		"write timeout negative": {MaxPayloadBytes: valid.MaxPayloadBytes, MaxInFlightRequests: valid.MaxInFlightRequests, ReadTimeout: valid.ReadTimeout, WriteTimeout: -time.Second},
	}
	for name, limits := range tests {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("NewServer accepted invalid resource configuration")
				}
			}()
			_ = NewServer(WithResourceLimits(limits))
		})
	}
}
