package tlrpc

import (
	"context"
	"testing"

	"github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
)

func TestTypeAliases(t *testing.T) {
	// Test that type aliases work correctly

	// Session type alias
	var sess *Session = &session.Session{}
	if sess == nil {
		t.Error("Session type alias not working")
	}

	// Transport type alias
	var tr Transport = &transport.TCPTransport{}
	if tr == nil {
		t.Error("Transport type alias not working")
	}

	// Listener type alias - interface, can't instantiate directly
	// Conn type alias - interface, can't instantiate directly
}

func TestServiceDesc(t *testing.T) {
	desc := ServiceDesc{
		ServiceName: "TestService",
		Methods: []MethodDesc{
			{
				MethodName: "TestMethod",
				Handler:    func() {},
			},
		},
	}

	if desc.ServiceName != "TestService" {
		t.Errorf("ServiceName not set correctly: got %s, want %s", desc.ServiceName, "TestService")
	}

	if len(desc.Methods) != 1 {
		t.Errorf("Methods slice length incorrect: got %d, want %d", len(desc.Methods), 1)
	}

	if desc.Methods[0].MethodName != "TestMethod" {
		t.Errorf("Method name not set correctly: got %s, want %s", desc.Methods[0].MethodName, "TestMethod")
	}
}

func TestMethodDesc(t *testing.T) {
	handler := func() { /* test handler */ }

	desc := MethodDesc{
		MethodName: "TestMethod",
		Handler:    handler,
	}

	if desc.MethodName != "TestMethod" {
		t.Errorf("MethodName not set correctly: got %s, want %s", desc.MethodName, "TestMethod")
	}

	if desc.Handler == nil {
		t.Error("Handler not set")
	}
}

func TestUnaryServerInfo(t *testing.T) {
	info := UnaryServerInfo{
		FullMethod: "/package.Service/Method",
	}

	if info.FullMethod != "/package.Service/Method" {
		t.Errorf("FullMethod not set correctly: got %s, want %s", info.FullMethod, "/package.Service/Method")
	}
}

// Test interfaces by creating mock implementations
type mockTLObjectForTypes struct {
	id uint32
}

func (m *mockTLObjectForTypes) ConstructorID() uint32 {
	return m.id
}

func TestTLObjectInterface(t *testing.T) {
	obj := &mockTLObjectForTypes{id: 0x12345678}

	var tlObj TLObject = obj
	if tlObj.ConstructorID() != 0x12345678 {
		t.Errorf("TLObject interface not working: got %x, want %x", tlObj.ConstructorID(), 0x12345678)
	}
}

type mockLoggerForTypes struct {
	messages []string
}

func (m *mockLoggerForTypes) Info(msg string, args ...interface{}) {
	m.messages = append(m.messages, "INFO: "+msg)
}

func (m *mockLoggerForTypes) Error(msg string, args ...interface{}) {
	m.messages = append(m.messages, "ERROR: "+msg)
}

func (m *mockLoggerForTypes) Debug(msg string, args ...interface{}) {
	m.messages = append(m.messages, "DEBUG: "+msg)
}

func TestLoggerInterface(t *testing.T) {
	logger := &mockLoggerForTypes{}

	var l Logger = logger
	l.Info("test info")
	l.Error("test error")
	l.Debug("test debug")

	if len(logger.messages) != 3 {
		t.Errorf("expected 3 log messages, got %d", len(logger.messages))
	}

	expected := []string{
		"INFO: test info",
		"ERROR: test error",
		"DEBUG: test debug",
	}

	for i, exp := range expected {
		if logger.messages[i] != exp {
			t.Errorf("log message %d incorrect: got %s, want %s", i, logger.messages[i], exp)
		}
	}
}

type mockHandshakeHandlerForTypes struct{}

func (m *mockHandshakeHandlerForTypes) HandleUnencrypted(ctx context.Context, msgID int64, data []byte) ([]byte, error) {
	return []byte("response"), nil
}

func TestHandshakeHandlerInterface(t *testing.T) {
	handler := &mockHandshakeHandlerForTypes{}

	var h HandshakeHandler = handler
	resp, err := h.HandleUnencrypted(nil, 123, []byte("request"))

	if err != nil {
		t.Errorf("HandleUnencrypted returned error: %v", err)
	}

	if string(resp) != "response" {
		t.Errorf("HandleUnencrypted returned wrong response: got %s, want %s", string(resp), "response")
	}
}

type mockAuthorizerForTypes struct {
	allow bool
}

func (m *mockAuthorizerForTypes) Authorize(ctx context.Context, req interface{}) error {
	if !m.allow {
		return NewUnauthorizedError("access denied")
	}
	return nil
}

func TestAuthorizerInterface(t *testing.T) {
	// Test allowing authorizer
	allowAuth := &mockAuthorizerForTypes{allow: true}
	err := allowAuth.Authorize(nil, "request")
	if err != nil {
		t.Errorf("allowing authorizer returned error: %v", err)
	}

	// Test denying authorizer
	denyAuth := &mockAuthorizerForTypes{allow: false}
	err = denyAuth.Authorize(nil, "request")
	if err == nil {
		t.Error("denying authorizer should return error")
	}
}

// Test function type definitions
func TestUnaryHandlerType(t *testing.T) {
	var handler UnaryHandler = func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	resp, err := handler(nil, "request")
	if err != nil {
		t.Errorf("UnaryHandler returned error: %v", err)
	}

	if resp != "response" {
		t.Errorf("UnaryHandler returned wrong response: got %v, want %v", resp, "response")
	}
}

func TestHandlerType(t *testing.T) {
	var handler Handler = func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	resp, err := handler(nil, "request")
	if err != nil {
		t.Errorf("Handler returned error: %v", err)
	}

	if resp != "response" {
		t.Errorf("Handler returned wrong response: got %v, want %v", resp, "response")
	}
}

func TestInterceptorType(t *testing.T) {
	var interceptor Interceptor = func(next Handler) Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			return next(ctx, req)
		}
	}

	originalHandler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	wrappedHandler := interceptor(originalHandler)
	resp, err := wrappedHandler(nil, "request")

	if err != nil {
		t.Errorf("Interceptor returned error: %v", err)
	}

	if resp != "response" {
		t.Errorf("Interceptor returned wrong response: got %v, want %v", resp, "response")
	}
}
