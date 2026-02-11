# Command Line Tools

This directory contains command-line utilities for working with TLRPC.

## Available Tools

### Code Generation
- `tlrpc-gen`: Generate Go code from TL schemas
- `tlrpc-layer-gen`: Generate layer-specific serialization code

### Development Tools
- `tlrpc-schema-validate`: Validate TL schema files
- `tlrpc-test-fixtures`: Generate test fixtures from schemas

### Server Tools
- `tlrpc-server`: Reference server implementation
- `tlrpc-client`: Command-line client for testing

## Usage

```bash
# Generate code from schema
go run ./cmd/tlrpc-gen -schema=path/to/schema.tl -output=generated/

# Start reference server
go run ./cmd/tlrpc-server -config=config.yaml

# Validate schema
go run ./cmd/tlrpc-schema-validate schema.tl
```

## Installation

Tools can be installed globally:

```bash
go install ./cmd/tlrpc-gen
go install ./cmd/tlrpc-server
```

## Development

When adding new tools:
1. Create a new directory under `cmd/`
2. Include a `main.go` with proper CLI interface
3. Add cobra or similar CLI framework for consistency
4. Include comprehensive help text and examples