// Package protocol validates decrypted client-to-server MTProto message
// metadata. It deliberately does not depend on TL object implementations,
// transports, encryption, sessions, or the root tlrpc package.
//
// Callers classify decoded bodies as content-related, non-content-related, or
// containers and pass only protocol metadata to Validator. Validation failures
// are returned as *BadMessageError values carrying the canonical MTProto error
// code and offending message metadata; encoding bad_msg_notification or
// bad_server_salt remains the caller's responsibility.
package protocol
