# MTProto Protocol

This package implements the core MTProto protocol logic, including message framing, acknowledgments, and resend handling.

## Overview

The protocol layer manages:
- Message ID generation (time-based)
- Sequence numbers for ordering
- Acknowledgment tracking and responses
- Resend request processing
- Message container handling
- Error handling and conversion

## Message Types

- **UnencryptedMessage**: Used only during initial handshake (routed via `tlrpc/handshake.go`)
- **EncryptedMessage**: Standard encrypted RPC messages
- **Container**: Batched message containers for efficiency

## Features

- **Reliability**: Automatic resend of unacknowledged messages
- **Ordering**: Message sequence number validation
- **Batching**: Container support for reduced round trips
- **Flow Control**: Acknowledgment-based flow control

## Integration

Works closely with the crypto layer for encryption/decryption and the session layer for state management.
