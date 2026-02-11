# Test Schemas

This directory contains TL schema files used for testing the parser and validator.

## Files

- `simple.tl`: Basic valid schema with constructors and functions
- `with_errors.tl`: Schema with validation errors (duplicate IDs, undefined types)
- `flags.tl`: Schema testing flag bit consistency
- `circular.tl`: Schema with circular type dependencies

## Usage

These files can be used to test the parser and validator:

```go
data, _ := os.ReadFile("testdata/schemas/simple.tl")
parser := codegen.NewParser(string(data))
schema, err := parser.Parse()
// Test parsing

validator := codegen.NewValidator(schema)
err = validator.Validate()
// Test validation
```