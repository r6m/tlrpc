package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r6m/tlrpc/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_SimpleSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "schemas", "simple.tl"))
	require.NoError(t, err)

	p := parser.NewParser(string(data))
	schema, err := p.Parse()
	require.NoError(t, err)

	v := parser.NewValidator(schema)
	err = v.Validate()
	assert.NoError(t, err)

	// Check that we parsed the expected elements
	assert.Len(t, schema.Types, 4) // Bool, User, auth.SentCode, auth.Authorization
	assert.Len(t, schema.Functions, 2)
	assert.True(t, len(schema.Constructors) > 0)
}

func TestIntegration_SchemaWithErrors(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "schemas", "with_errors.tl"))
	require.NoError(t, err)

	p := parser.NewParser(string(data))
	schema, err := p.Parse()
	require.NoError(t, err)

	v := parser.NewValidator(schema)
	err = v.Validate()
	assert.Error(t, err)

	errors := v.Errors()
	assert.True(t, len(errors) > 0, "Should have validation errors")
}

func TestIntegration_FlagsSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "schemas", "flags.tl"))
	require.NoError(t, err)

	p := parser.NewParser(string(data))
	schema, err := p.Parse()
	require.NoError(t, err)

	v := parser.NewValidator(schema)
	err = v.Validate()
	assert.Error(t, err, "Should have flag consistency errors")

	errors := v.Errors()
	flagErrors := 0
	for _, e := range errors {
		if strings.Contains(e.Message, "multiple parameters using flag bit") {
			flagErrors++
		}
	}
	assert.True(t, flagErrors > 0, "Should have flag bit conflict errors")
}

func TestIntegration_CircularSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "schemas", "circular.tl"))
	require.NoError(t, err)

	p := parser.NewParser(string(data))
	schema, err := p.Parse()
	require.NoError(t, err)

	v := parser.NewValidator(schema)
	err = v.Validate()
	assert.Error(t, err, "Should have circular dependency errors")

	errors := v.Errors()
	circularErrors := 0
	for _, e := range errors {
		if strings.Contains(e.Message, "circular type dependency") {
			circularErrors++
		}
	}
	assert.True(t, circularErrors > 0, "Should have circular dependency errors")
}

func TestIntegration_RealSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "schema-217.tl"))
	require.NoError(t, err)

	parser := parser.NewParser(string(data))
	schema, err := parser.Parse()
	require.NoError(t, err)

	assert.True(t, len(schema.Types) > 0)
	assert.True(t, len(schema.Constructors) > 0)
}
