# Crypto Engine

This package implements the complete cryptographic operations required by MTProto v2, including encryption, decryption, key management, and the full Diffie-Hellman handshake.

## Overview

The crypto layer handles:
- AES-256-IGE encryption/decryption for messages with MTProto 2.0 key derivation
- Complete MTProto v2 handshake implementation (RSA + DH key exchange)
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
