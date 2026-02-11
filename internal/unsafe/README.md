# Unsafe Operations

Internal package containing unsafe operations for zero-copy optimizations.

## Overview

This package provides utilities for zero-copy operations where memory safety can be guaranteed through careful usage patterns.

## Operations

- **Zero-copy deserialization**: Direct buffer-to-struct conversion
- **Slice manipulation**: Unsafe slice operations for performance
- **Memory pinning**: Prevent GC during critical operations

## Safety

All operations in this package require careful review and testing. Usage is restricted to performance-critical paths where the safety guarantees can be maintained.

## Usage Guidelines

- Only use when profiling shows allocation is a bottleneck
- Include comprehensive tests for each unsafe operation
- Document safety assumptions clearly
- Consider alternatives before using unsafe operations

## Alternatives

Prefer safe operations in `internal/pool` when possible. Only reach for unsafe operations after measuring performance impact.