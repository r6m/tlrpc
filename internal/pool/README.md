# Object Pool

Internal package providing object pooling for performance optimization.

## Overview

This package implements object pooling to reduce garbage collection pressure and improve performance in hot paths.

## Pools

- **Buffer Pool**: Reusable byte buffers for serialization
- **Message Pool**: Pooled message structures
- **TL Object Pool**: Generic TL object pooling
- **Context Pool**: Request context pooling

## Usage

Pools are automatically used by the framework components but can also be used directly:

```go
buf := pool.GetBuffer()
defer pool.PutBuffer(buf)
// use buf...
```

## Synchronization

All pools are thread-safe and use appropriate synchronization primitives for concurrent access.

## Memory Management

Pools include size limits and automatic cleanup to prevent unbounded memory growth.