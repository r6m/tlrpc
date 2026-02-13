# Command Line Tools

This directory contains command-line utilities for working with TLRPC.

## Available Tools

### Code Generation
- `tlrpc-gen`: Generate Go code from TL schemas and Generate layer-specific serialization

## Usage

```bash
# Generate code from schema
go run ./cmd/tlrpc-gen -schema=path/to/schema.tl -output=generated/

```

## Installation

Tools can be installed globally:

```bash
go install ./cmd/tlrpc-gen
```

## Development

When adding new tools:
1. Create a new directory under `cmd/`
2. Include a `main.go` with proper CLI interface
3. Add cobra or similar CLI framework for consistency
4. Include comprehensive help text and examples