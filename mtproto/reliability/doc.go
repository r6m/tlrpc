// Package reliability provides bounded in-memory tracking of sent MTProto
// messages for acknowledgement and resend handling.
//
// Store does not start background goroutines. Expired entries are removed
// synchronously whenever the store is accessed, or explicitly through Expire.
package reliability
