package tlrpc

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// mockAuthorizer implements Authorizer interface for testing
type mockAuthorizer struct {
	shouldFail bool
}

func (m *mockAuthorizer) Authorize(ctx context.Context, req interface{}) error {
	if m.shouldFail {
		return errors.New("authorization failed")
	}
	return nil
}

func TestChainUnaryInterceptors(t *testing.T) {
	callOrder := []string{}

	interceptor1 := func(ctx context.Context, req interface{}, info *UnaryServerInfo, handler UnaryHandler) (interface{}, error) {
		callOrder = append(callOrder, "interceptor1")
		return handler(ctx, req)
	}

	interceptor2 := func(ctx context.Context, req interface{}, info *UnaryServerInfo, handler UnaryHandler) (interface{}, error) {
		callOrder = append(callOrder, "interceptor2")
		return handler(ctx, req)
	}

	chained := ChainUnaryInterceptors(interceptor1, interceptor2)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		callOrder = append(callOrder, "handler")
		return "response", nil
	}

	info := &UnaryServerInfo{FullMethod: "test.method"}
	resp, err := chained(context.Background(), "request", info, handler)

	if err != nil {
		t.Errorf("chained interceptor returned error: %v", err)
	}

	if resp != "response" {
		t.Errorf("chained interceptor returned wrong response: got %v, want %v", resp, "response")
	}

	// Verify call order (interceptors applied in reverse order, so interceptor1 is outermost)
	expectedOrder := []string{"interceptor1", "interceptor2", "handler"}
	if len(callOrder) != len(expectedOrder) {
		t.Errorf("wrong call order length: got %d, want %d", len(callOrder), len(expectedOrder))
	}

	for i, expected := range expectedOrder {
		if i >= len(callOrder) || callOrder[i] != expected {
			t.Errorf("wrong call order at position %d: got %v, want %v", i, callOrder, expectedOrder)
		}
	}
}

func TestRecoveryInterceptor(t *testing.T) {
	const panicMsg = "sentinel-panic-secret"
	interceptor := RecoveryInterceptor()

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		panic(panicMsg)
	}

	info := &UnaryServerInfo{FullMethod: "test.method"}
	_, err := interceptor(context.Background(), "request", info, handler)

	if err == nil {
		t.Error("expected error from recovery interceptor")
	}

	rpcErr, ok := IsRPCError(err)
	if !ok || rpcErr.ErrorCode != int32(Internal) || rpcErr.ErrorMessage != "INTERNAL" {
		t.Fatalf("recovery error = %v, want 500 INTERNAL", err)
	}
	if strings.Contains(err.Error(), panicMsg) {
		t.Fatal("panic value was exposed by recovery interceptor")
	}
}

func TestRecoveryInterceptorNoPanic(t *testing.T) {
	interceptor := RecoveryInterceptor()

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}

	info := &UnaryServerInfo{FullMethod: "test.method"}
	resp, err := interceptor(context.Background(), "request", info, handler)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if resp != "success" {
		t.Errorf("wrong response: got %v, want %v", resp, "success")
	}
}

func TestLoggingInterceptor(t *testing.T) {
	logMsgs := []string{}

	logger := &mockLogger{
		infoMsgs:  logMsgs,
		errorMsgs: logMsgs,
	}

	interceptor := LoggingInterceptor(logger)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}

	info := &UnaryServerInfo{FullMethod: "test.method"}
	resp, err := interceptor(context.Background(), "request", info, handler)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if resp != "success" {
		t.Errorf("wrong response: got %v, want %v", resp, "success")
	}

	// Should have logged request and response
	if len(logger.infoMsgs) != 2 {
		t.Errorf("expected 2 info log messages, got %d", len(logger.infoMsgs))
	}
}

func TestLoggingInterceptorWithError(t *testing.T) {
	logger := &mockLogger{}

	interceptor := LoggingInterceptor(logger)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, errors.New("handler error")
	}

	info := &UnaryServerInfo{FullMethod: "test.method"}
	_, err := interceptor(context.Background(), "request", info, handler)

	if err == nil {
		t.Error("expected error from handler")
	}

	// Should have logged request and error
	if len(logger.infoMsgs) != 1 {
		t.Errorf("expected 1 info log message, got %d", len(logger.infoMsgs))
	}
	if len(logger.errorMsgs) != 1 {
		t.Errorf("expected 1 error log message, got %d", len(logger.errorMsgs))
	}
}

func TestLoggingInterceptorNilLogger(t *testing.T) {
	interceptor := LoggingInterceptor(nil)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}

	info := &UnaryServerInfo{FullMethod: "test.method"}
	resp, err := interceptor(context.Background(), "request", info, handler)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if resp != "success" {
		t.Errorf("wrong response: got %v, want %v", resp, "success")
	}

	// Should not panic with nil logger
}

func TestAuthInterceptor(t *testing.T) {
	authorizer := &mockAuthorizer{shouldFail: false}

	interceptor := AuthInterceptor(authorizer)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "authorized", nil
	}

	info := &UnaryServerInfo{FullMethod: "test.method"}
	resp, err := interceptor(context.Background(), "request", info, handler)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if resp != "authorized" {
		t.Errorf("wrong response: got %v, want %v", resp, "authorized")
	}
}

func TestAuthInterceptorFailure(t *testing.T) {
	authorizer := &mockAuthorizer{shouldFail: true}

	interceptor := AuthInterceptor(authorizer)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "should not reach here", nil
	}

	info := &UnaryServerInfo{FullMethod: "test.method"}
	resp, err := interceptor(context.Background(), "request", info, handler)

	if err == nil {
		t.Error("expected authorization error")
	}

	if resp != nil {
		t.Errorf("expected nil response on auth failure, got %v", resp)
	}

	if err.Error() != "authorization failed" {
		t.Errorf("wrong error message: got %v, want %v", err.Error(), "authorization failed")
	}
}

func TestAuthInterceptorNilAuthorizer(t *testing.T) {
	interceptor := AuthInterceptor(nil)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "no auth required", nil
	}

	info := &UnaryServerInfo{FullMethod: "test.method"}
	resp, err := interceptor(context.Background(), "request", info, handler)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if resp != "no auth required" {
		t.Errorf("wrong response: got %v, want %v", resp, "no auth required")
	}
}

// Test context helper functions that aren't covered by context_test.go
func TestContextHelpersNilContext(t *testing.T) {
	ctx := context.Background()
	if LayerFromContext(ctx) != 0 {
		t.Error("LayerFromContext should return 0 for empty context")
	}
	if AuthKeyIDFromContext(ctx) != 0 {
		t.Error("AuthKeyIDFromContext should return 0 for empty context")
	}
	if UserIDFromContext(ctx) != 0 {
		t.Error("UserIDFromContext should return 0 for empty context")
	}
}

func TestWithContextHelpers(t *testing.T) {
	ctx := context.Background()

	ctx = withLayer(ctx, 42)
	ctx = withAuthKeyID(ctx, 456)
	ctx = withUserID(ctx, 789)

	if LayerFromContext(ctx) != 42 {
		t.Error("withLayer/LayerFromContext round trip failed")
	}
	if AuthKeyIDFromContext(ctx) != 456 {
		t.Error("withAuthKeyID/AuthKeyIDFromContext round trip failed")
	}
	if UserIDFromContext(ctx) != 789 {
		t.Error("withUserID/UserIDFromContext round trip failed")
	}
}
