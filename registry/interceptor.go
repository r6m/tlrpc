package registry

import (
	"context"
	"fmt"
)

// Logger provides structured logging hooks.
type Logger interface {
	Info(msg string, args ...interface{})
	Error(msg string, args ...interface{})
	Debug(msg string, args ...interface{})
}

// Authorizer validates a request and returns an error if unauthorized.
type Authorizer interface {
	Authorize(ctx context.Context, req interface{}) error
}

// ChainInterceptors chains multiple interceptors together.
func ChainInterceptors(interceptors ...Interceptor) Interceptor {
	return func(next Handler) Handler {
		for i := len(interceptors) - 1; i >= 0; i-- {
			next = interceptors[i](next)
		}
		return next
	}
}

// RecoveryInterceptor recovers panics and uses errorFactory to build errors.
func RecoveryInterceptor(errorFactory func(message string) error) Interceptor {
	return func(next Handler) Handler {
		return func(ctx context.Context, req interface{}) (resp interface{}, err error) {
			defer func() {
				if r := recover(); r != nil {
					if errorFactory != nil {
						err = errorFactory(fmt.Sprintf("panic: %v", r))
						return
					}
					err = fmt.Errorf("panic: %v", r)
				}
			}()
			return next(ctx, req)
		}
	}
}

// LoggingInterceptor logs request/response lifecycle.
func LoggingInterceptor(logger Logger) Interceptor {
	return func(next Handler) Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			if logger != nil {
				logger.Info("tlrpc request", "type", fmt.Sprintf("%T", req))
			}
			resp, err := next(ctx, req)
			if logger != nil {
				if err != nil {
					logger.Error("tlrpc response", "error", err)
				} else {
					logger.Info("tlrpc response", "type", fmt.Sprintf("%T", resp))
				}
			}
			return resp, err
		}
	}
}

// AuthInterceptor checks authorization.
func AuthInterceptor(authorizer Authorizer) Interceptor {
	return func(next Handler) Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			if authorizer == nil {
				return next(ctx, req)
			}
			if err := authorizer.Authorize(ctx, req); err != nil {
				return nil, err
			}
			return next(ctx, req)
		}
	}
}
