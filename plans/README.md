# TLRPC Development Plans

TLRPC is a gRPC-inspired framework for building Telegram-compatible RPC servers. This directory contains plans for developing and maintaining the framework.

## Current Status

TLRPC is a working gRPC-like framework with:

- **Code Generation**: `tlrpc-gen` generates Go types and services from TL schemas (like `protoc` for Protocol Buffers)
- **Service Pattern**: Familiar `Unimplemented*Server` embedding pattern like gRPC
- **MTProto Gateway**: Complete MTProto v2 protocol implementation for Telegram clients
- **Interceptor Chain**: Middleware support like gRPC interceptors
- **Registry System**: Constructor-based TL object encoding/decoding

## Key Principles

1. **gRPC-like Developer Experience**: TL schemas → generated code → service implementations → server registration
2. **MTProto Compatibility**: Full protocol stack for Android/web Telegram clients
3. **Simple Architecture**: Clean separation between protocol plumbing and business logic
4. **Type Safety**: Compile-time guarantees for all TL types

## Active Development Areas

### Core Infrastructure (Implemented)
- [x] TL schema parsing and AST
- [x] Go code generation (`tlrpc-gen`)
- [x] MTProto serialization primitives
- [x] Transport layer (TCP/WebSocket)
- [x] Cryptographic primitives (AES-IGE, RSA, DH)
- [x] Session management
- [x] Codec registry system
- [x] Server framework with interceptors

### Documentation & Examples
- [x] README with gRPC analogy
- [x] Tutorial for gRPC-like workflow
- [x] API documentation
- [ ] Complete examples for common use cases
- [ ] Performance benchmarks and tuning guide

### Production Readiness
- [ ] Full MTProto handshake implementation (currently minimal)
- [ ] Production session storage backends
- [ ] Metrics and monitoring integration
- [ ] Rate limiting and security features
- [ ] Load testing and performance optimization

## Request Flow

```
Telegram Client → MTProto Transport → TLRPC Server → Your Service Implementation → Response → Client
```

This is intentionally similar to gRPC's flow but handles MTProto protocol details automatically.

## Contributing

When proposing changes, consider:
- Does this maintain the gRPC-like developer experience?
- Does this improve MTProto compatibility?
- Does this simplify the service implementation pattern?
- Does this enhance type safety or performance?

See individual plan files for specific implementation details.