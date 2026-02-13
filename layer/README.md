# Layer Support (Planned)

This package is reserved for future layer abstractions. Today, layer handling is implemented through the `codec` package and the `Codec` interface in `tlrpc`.

## Overview

When implemented, this package is expected to provide:
- Per-layer type serialization registries
- Constructor ID routing based on client layer
- Layer version tracking and validation

## Current Approach

- Use `codec.Registry` to map constructor IDs to concrete TL types.
- Implement per-layer behavior in a custom `Codec` (the `layer` argument is passed to `Decode`/`Encode`).

## Design Principles

- **No Automatic Conversion**: Layer differences are handled explicitly by user code
- **Generated Code**: All serialization code is generated from TL schemas
- **Constructor Routing**: Each layer has its own constructor ID mappings
- **Layer Transparency**: Framework handles layer differences automatically

## Usage

Each supported Telegram layer can be expressed as a distinct registry or codec implementation.
