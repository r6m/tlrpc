// Package mtproto provides RPC metadata structures.
package mtproto

// RPCMetadata contains metadata about an RPC call.
type RPCMetadata struct {
	MethodName string
	Args       []interface{}
	Response   interface{}
	Error      error
}

// NewRPCMetadata creates new RPC metadata.
func NewRPCMetadata(methodName string, args []interface{}) *RPCMetadata {
	return &RPCMetadata{
		MethodName: methodName,
		Args:       args,
	}
}

// SetResponse sets the response.
func (r *RPCMetadata) SetResponse(response interface{}) {
	r.Response = response
}

// SetError sets the error.
func (r *RPCMetadata) SetError(err error) {
	r.Error = err
}

// RPCResult represents an RPC result.
type RPCResult struct {
	ReqMsgID int64
	Result   TLObject
}

// NewRPCResult creates a new RPC result.
func NewRPCResult(reqMsgID int64, result TLObject) *RPCResult {
	return &RPCResult{
		ReqMsgID: reqMsgID,
		Result:   result,
	}
}

// RPCError represents an RPC error.
type RPCError struct {
	ErrorCode    int32
	ErrorMessage string
}

// NewRPCError creates a new RPC error.
func NewRPCError(code int32, message string) *RPCError {
	return &RPCError{
		ErrorCode:    code,
		ErrorMessage: message,
	}
}

// Error implements the error interface.
func (e *RPCError) Error() string {
	return e.ErrorMessage
}

// Code returns the error code.
func (e *RPCError) Code() int32 {
	return e.ErrorCode
}