# Crypto Engine

This package implements the cryptographic operations required by MTProto, including encryption, decryption, and key management. The handshake in this repo is simplified and is not a full MTProto implementation.

## Overview

The crypto layer handles:
- AES-256-IGE encryption/decryption for messages
- Simplified handshake primitives (see `tlrpc/handshake.go` for server flow)
- Auth key derivation and management
- Session key generation

## Key Types

- **AuthKey**: Permanent authentication key from initial handshake
- **TempAuthKey**: Optional perfect forward secrecy keys
- **SessionKey**: Per-session derived encryption keys

## Algorithms

- **AES-256-IGE**: For message encryption/decryption
- **RSA**: For initial key exchange verification
- **DH**: For secure key agreement

## Security

This package implements the security-critical portions of MTProto and must be thoroughly audited for correctness.
