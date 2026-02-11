package tlrpc

import (
	"errors"
	"fmt"
)

// Common errors
var (
	ErrUnauthorized     = errors.New("tlrpc: unauthorized")
	ErrInvalidLayer     = errors.New("tlrpc: invalid layer")
	ErrMethodNotFound   = errors.New("tlrpc: method not found")
	ErrInvalidRequest   = errors.New("tlrpc: invalid request")
	ErrInternalError    = errors.New("tlrpc: internal error")
	ErrTimeout          = errors.New("tlrpc: timeout")
	ErrRateLimited      = errors.New("tlrpc: rate limited")
)

// RPCError represents an RPC error with a code and message.
type RPCError struct {
	Code    int
	Message string
}

// Error implements the error interface.
func (e *RPCError) Error() string {
	return fmt.Sprintf("RPC error %d: %s", e.Code, e.Message)
}

// NewRPCError creates a new RPC error.
func NewRPCError(code int, message string) *RPCError {
	return &RPCError{
		Code:    code,
		Message: message,
	}
}

// IsRPCError checks if an error is an RPC error.
func IsRPCError(err error) bool {
	_, ok := err.(*RPCError)
	return ok
}

// ErrorCode extracts the error code from an RPC error.
func ErrorCode(err error) int {
	if rpcErr, ok := err.(*RPCError); ok {
		return rpcErr.Code
	}
	return -1
}