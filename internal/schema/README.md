# Schema Parser

Internal package for parsing and processing Telegram Type Language (TL) schemas.

## Overview

This package provides:
- TL schema parsing and AST generation
- Type definition extraction
- Constructor and method analysis
- Schema validation and type checking
- Layer-specific schema processing

## Key Components

- **Parser**: Converts TL source text to AST
- **Validator**: Checks schema consistency and correctness
- **Analyzer**: Extracts type relationships and dependencies
- **Generator**: Prepares schema data for code generation

## Usage

Primarily used by code generation tools to understand TL type definitions and generate appropriate Go code.

## Architecture

Works closely with `internal/codegen` to transform parsed schemas into runnable Go code.