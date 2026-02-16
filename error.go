// Package tlrpc provides MTProto-compatible RPC error handling.
package tlrpc

import (
	"fmt"
)

// Code represents an MTProto RPC error code.
// Based on Telegram's standard error codes.
type Code int32

const (
	// OK is returned on success.
	OK Code = 0

	// MTProto standard error codes
	// See: https://core.telegram.org/api/errors

	// 303 - SEE_OTHER
	// The request must be repeated, but directed to a different data center or to a different server.
	SeeOther Code = 303

	// 400 - BAD_REQUEST
	// The query contains errors. In the event that a request was created
	// using a form and contains user generated data, the user should be
	// notified that the data must be corrected before the query is repeated.
	BadRequest Code = 400

	// 401 - UNAUTHORIZED
	// There was an unauthorized attempt to use functionality available only to authorized users.
	Unauthorized Code = 401

	// 403 - FORBIDDEN
	// Privacy violation. For example, an attempt to write a message to someone
	// who has blacklisted the current user.
	Forbidden Code = 403

	// 404 - NOT_FOUND
	// An attempt to invoke a non-existent object, such as a method.
	NotFound Code = 404

	// 420 - FLOOD
	// The maximum allowed number of attempts to invoke the given method
	// with the given parameters has been exceeded.
	Flood Code = 420

	// 500 - INTERNAL
	// An internal server error occurred while a request was being processed.
	Internal Code = 500

	// Additional common codes used in practice
	Canceled         Code = 499
	DeadlineExceeded Code = 504
	Unavailable      Code = 503
	Unknown          Code = 520
)

// RPCError represents an MTProto RPC error.
// Constructor ID: 0x2144ca19
type RPCError struct {
	ErrorCode    int32  // MTProto error code
	ErrorMessage string // Human-readable error message
}

// ConstructorID returns the TL constructor ID for rpc_error.
func (e *RPCError) ConstructorID() uint32 {
	return 0x2144ca19 // rpc_error
}

// Method returns an empty string for RPCError (not an RPC method).
func (e *RPCError) Method() string {
	return ""
}

// Error implements the error interface.
func (e *RPCError) Error() string {
	return fmt.Sprintf("RPC_ERROR %d: %s", e.ErrorCode, e.ErrorMessage)
}

// NewRPCError creates a new MTProto RPC error.
func NewRPCError(code int32, message string) *RPCError {
	return &RPCError{
		ErrorCode:    code,
		ErrorMessage: message,
	}
}

// NewStatus creates a new RPC error (for backward compatibility with gRPC-like API).
func NewStatus(code Code, msg string) *RPCError {
	return NewRPCError(int32(code), msg)
}

// SerializeTL implements TL serialization for RPC errors.
func (e *RPCError) SerializeTL(w TLWriter) error {
	if err := w.WriteUint32(e.ConstructorID()); err != nil {
		return err
	}
	if err := w.WriteInt32(e.ErrorCode); err != nil {
		return err
	}
	return w.WriteString(e.ErrorMessage)
}

// DeserializeTL implements TL deserialization for RPC errors.
func (e *RPCError) DeserializeTL(r TLReader) error {
	ctorID, err := r.ReadUint32()
	if err != nil {
		return err
	}
	if ctorID != e.ConstructorID() {
		return fmt.Errorf("wrong constructor ID for RPCError: got %x, want %x", ctorID, e.ConstructorID())
	}
	errorCode, err := r.ReadInt32()
	if err != nil {
		return err
	}
	errorMessage, err := r.ReadString()
	if err != nil {
		return err
	}
	e.ErrorCode = errorCode
	e.ErrorMessage = errorMessage
	return nil
}

// TLWriter and TLReader interfaces for serialization
type TLWriter interface {
	WriteUint32(v uint32) error
	WriteInt32(v int32) error
	WriteString(v string) error
}

type TLReader interface {
	ReadUint32() (uint32, error)
	ReadInt32() (int32, error)
	ReadString() (string, error)
}

// Common MTProto RPC error constructors
var (
	// Standard MTProto errors
	ErrSeeOther     = NewRPCError(303, "SEE_OTHER")
	ErrBadRequest   = NewRPCError(400, "BAD_REQUEST")
	ErrUnauthorized = NewRPCError(401, "UNAUTHORIZED")
	ErrForbidden    = NewRPCError(403, "FORBIDDEN")
	ErrNotFound     = NewRPCError(404, "NOT_FOUND")
	ErrFlood        = NewRPCError(420, "FLOOD")
	ErrInternal     = NewRPCError(500, "INTERNAL")

	// Additional common errors
	ErrCanceled         = NewRPCError(499, "CANCELED")
	ErrDeadlineExceeded = NewRPCError(504, "DEADLINE_EXCEEDED")
	ErrUnavailable      = NewRPCError(503, "UNAVAILABLE")
	ErrUnknown          = NewRPCError(520, "UNKNOWN")

	// Legacy gRPC-style aliases for backward compatibility
	ErrInvalidArgument    = ErrBadRequest
	ErrPermissionDenied   = ErrForbidden
	ErrResourceExhausted  = ErrFlood
	ErrFailedPrecondition = ErrBadRequest
	ErrAborted            = ErrInternal
	ErrOutOfRange         = ErrBadRequest
	ErrUnimplemented      = NewRPCError(501, "NOT_IMPLEMENTED")
	ErrDataLoss           = ErrInternal
	ErrUnauthenticated    = ErrUnauthorized
)

// Errorf creates a new RPC error with formatted message.
func Errorf(code Code, format string, args ...interface{}) error {
	return NewRPCError(int32(code), fmt.Sprintf(format, args...))
}

// FromError converts an error to RPCError.
func FromError(err error) *RPCError {
	if err == nil {
		return NewRPCError(0, "")
	}
	if rpcErr, ok := err.(*RPCError); ok {
		return rpcErr
	}
	return NewRPCError(int32(Unknown), err.Error())
}

// IsRPCError checks if error is an RPCError.
func IsRPCError(err error) (*RPCError, bool) {
	if rpcErr, ok := err.(*RPCError); ok {
		return rpcErr, true
	}
	return nil, false
}

// Helper functions for common MTProto error patterns

// NewBadRequestError creates a BAD_REQUEST error with message.
func NewBadRequestError(message string) *RPCError {
	return NewRPCError(400, message)
}

// NewUnauthorizedError creates an UNAUTHORIZED error with message.
func NewUnauthorizedError(message string) *RPCError {
	return NewRPCError(401, message)
}

// NewForbiddenError creates a FORBIDDEN error with message.
func NewForbiddenError(message string) *RPCError {
	return NewRPCError(403, message)
}

// NewNotFoundError creates a NOT_FOUND error with message.
func NewNotFoundError(message string) *RPCError {
	return NewRPCError(404, message)
}

// NewFloodError creates a FLOOD error with message.
func NewFloodError(message string) *RPCError {
	return NewRPCError(420, message)
}

// NewInternalError creates an INTERNAL error with message.
func NewInternalError(message string) *RPCError {
	return NewRPCError(500, message)
}
