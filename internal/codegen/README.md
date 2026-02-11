# Code Generator

Internal package responsible for generating Go code from parsed TL schemas.

## Overview

This package generates:
- TL type definitions as Go structs
- Serialization/deserialization methods
- Constructor ID constants
- Layer-specific implementations
- Service interface definitions

## Generated Code Structure

- **Types**: Go structs matching TL definitions
- **Serialization**: Encode/decode methods for each type
- **Constructors**: Maps from constructor IDs to types
- **Services**: RPC method interfaces and clients

## Templates

Uses Go templates to generate consistent, idiomatic Go code with proper:
- Package structure
- Import management
- Error handling
- Documentation comments

## Integration

Called by command-line tools in `cmd/` and integrates with `internal/schema` for parsing.