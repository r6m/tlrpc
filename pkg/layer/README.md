# Layer Adapter

This package provides the layer abstraction that handles different Telegram client layer versions and their serialization formats.

## Overview

The layer adapter manages:
- Per-layer type serialization/deserialization
- Constructor ID routing based on client layer
- Layer version tracking and validation
- Type-safe object handling across layers

## Interface

```go
type Layer interface {
    Version() int
    Deserialize(constructorID uint32, data []byte) (TLObject, error)
    Serialize(obj TLObject) ([]byte, error)
    GetConstructorID(obj TLObject) uint32
}
```

## Design Principles

- **No Automatic Conversion**: Layer differences are handled explicitly by user code
- **Generated Code**: All serialization code is generated from TL schemas
- **Constructor Routing**: Each layer has its own constructor ID mappings
- **Layer Transparency**: Framework handles layer differences automatically

## Usage

Each supported Telegram layer gets its own generated implementation, allowing seamless handling of clients at different protocol versions.