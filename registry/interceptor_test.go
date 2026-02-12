package registry

import (
	"context"
	"errors"
	"testing"
)

type testAuthorizer struct {
	err error
}

func (a testAuthorizer) Authorize(ctx context.Context, req interface{}) error {
	return a.err
}

type testLogger struct {
	info  int
	error int
}

func (l *testLogger) Info(msg string, args ...interface{})  { l.info++ }
func (l *testLogger) Error(msg string, args ...interface{}) { l.error++ }
func (l *testLogger) Debug(msg string, args ...interface{}) {}

func TestChainInterceptorsOrder(t *testing.T) {
	steps := make([]string, 0)
	first := func(next Handler) Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			steps = append(steps, "first")
			return next(ctx, req)
		}
	}
	second := func(next Handler) Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			steps = append(steps, "second")
			return next(ctx, req)
		}
	}
	base := func(ctx context.Context, req interface{}) (interface{}, error) {
		steps = append(steps, "handler")
		return nil, nil
	}

	chain := ChainInterceptors(first, second)
	_, _ = chain(base)(context.Background(), nil)

	if len(steps) != 3 || steps[0] != "first" || steps[1] != "second" || steps[2] != "handler" {
		t.Fatalf("unexpected order: %v", steps)
	}
}

func TestRecoveryInterceptor(t *testing.T) {
	base := func(ctx context.Context, req interface{}) (interface{}, error) {
		panic("boom")
	}
	resp, err := RecoveryInterceptor(func(message string) error { return errors.New(message) })(base)(context.Background(), nil)
	if resp != nil {
		t.Fatalf("expected nil response")
	}
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestLoggingInterceptor(t *testing.T) {
	logger := &testLogger{}
	base := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	_, _ = LoggingInterceptor(logger)(base)(context.Background(), "req")
	if logger.info < 2 {
		t.Fatalf("expected info logs")
	}
}

func TestAuthInterceptor(t *testing.T) {
	authorizer := testAuthorizer{err: errors.New("nope")}
	base := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	_, err := AuthInterceptor(authorizer)(base)(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected auth error")
	}
}
