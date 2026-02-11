package tlrpc

import (
	"context"
)

// ChainInterceptors chains multiple interceptors into one.
func ChainInterceptors(interceptors ...Interceptor) Interceptor {
	return func(next Handler) Handler {
		for i := len(interceptors) - 1; i >= 0; i-- {
			next = interceptors[i](next)
		}
		return next
	}
}

// LoggingInterceptor logs RPC calls.
func LoggingInterceptor(logger func(format string, args ...interface{})) Interceptor {
	return func(next Handler) Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			logger("RPC call: %T", req)
			resp, err := next(ctx, req)
			if err != nil {
				logger("RPC error: %v", err)
			} else {
				logger("RPC response: %T", resp)
			}
			return resp, err
		}
	}
}

// RecoveryInterceptor recovers from panics in handlers.
func RecoveryInterceptor() Interceptor {
	return func(next Handler) Handler {
		return func(ctx context.Context, req interface{}) (resp interface{}, err error) {
			defer func() {
				if r := recover(); r != nil {
					// TODO: Log panic
					err = ErrInternalError
				}
			}()
			return next(ctx, req)
		}
	}
}

// AuthInterceptor checks authentication.
func AuthInterceptor(authFunc func(ctx context.Context) error) Interceptor {
	return func(next Handler) Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			if err := authFunc(ctx); err != nil {
				return nil, err
			}
			return next(ctx, req)
		}
	}
}