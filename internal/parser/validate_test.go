package parser

import (
	"strings"
	"testing"

	"github.com/r6m/tlrpc/internal/naming"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidator_ValidateValidSchema(t *testing.T) {
	input := `---types---
user#8f97c628 flags:# id:long first_name:flags.0?string = User;
userEmpty#d3bc4b7c = User;
sentCode#5e002502 type:string phone_code_hash:string = auth.SentCode;

---functions---
auth.sendCode#a677244f phone_number:string = auth.SentCode;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	validator := NewValidator(schema)
	err = validator.Validate()
	assert.NoError(t, err)
	assert.Empty(t, validator.Errors())
}

func TestValidator_ValidateDuplicateConstructorIDs(t *testing.T) {
	input := `---types---
user1#12345678 id:long = User;
user2#12345678 name:string = User;
testType#99999999 = TestType;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	validator := NewValidator(schema)
	err = validator.Validate()
	assert.Error(t, err)

	errors := validator.Errors()
	// Should have 2 duplicate ID errors, ignore other validation errors
	duplicateErrors := 0
	for _, e := range errors {
		if strings.Contains(e.Message, "duplicate constructor ID") {
			duplicateErrors++
		}
	}
	assert.Equal(t, 2, duplicateErrors)
}

func TestValidator_ValidateDuplicateFunctionIDs(t *testing.T) {
	input := `---types---
sentCode#99999999 = auth.SentCode;

---functions---
auth.sendCode1#12345678 = auth.SentCode;
auth.sendCode2#12345678 = auth.SentCode;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	validator := NewValidator(schema)
	err = validator.Validate()
	assert.Error(t, err)

	errors := validator.Errors()
	// Should have 2 duplicate ID errors, ignore undefined type errors
	duplicateErrors := 0
	for _, e := range errors {
		if strings.Contains(e.Message, "duplicate function ID") {
			duplicateErrors++
		}
	}
	assert.Equal(t, 2, duplicateErrors)
}

func TestValidator_ValidateUndefinedTypes(t *testing.T) {
	input := `---types---
user#123 id:UndefinedType = User;
testType#99999999 = TestType;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	validator := NewValidator(schema)
	err = validator.Validate()
	assert.Error(t, err)

	errors := validator.Errors()
	// Should have undefined type error
	found := false
	for _, e := range errors {
		if strings.Contains(e.Message, "undefined type UndefinedType") {
			found = true
			break
		}
	}
	assert.True(t, found, "Should contain undefined type error")
}

func TestValidator_ValidateBuiltinTypes(t *testing.T) {
	input := `---types---
user#123 id:int name:string = User;
testType#99999999 = TestType;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	validator := NewValidator(schema)
	err = validator.Validate()
	assert.NoError(t, err)
}

func TestValidator_ValidateFlagConsistency(t *testing.T) {
	input := `---types---
user#123 flags:# first_name:flags.0?string last_name:flags.0?string = User;
testType#99999999 = TestType;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	validator := NewValidator(schema)
	err = validator.Validate()
	assert.NoError(t, err)
}

func TestValidator_ValidateCircularDependencies(t *testing.T) {
	input := `---types---
typeA#123 refB:TypeB = TypeA;
typeB#456 refA:TypeA = TypeB;
typeC#789 = TypeC;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	validator := NewValidator(schema)
	err = validator.Validate()
	assert.NoError(t, err)
}

func TestValidator_ValidateNamespaceValidity(t *testing.T) {
	input := `---types---
user#123 = User;
sentCode#99999999 = auth.SentCode;

---functions---
auth.123invalid#a677244f = auth.SentCode;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	validator := NewValidator(schema)
	err = validator.Validate()
	assert.Error(t, err)

	errors := validator.Errors()
	// Should have invalid name format error
	found := false
	for _, e := range errors {
		if strings.Contains(e.Message, "invalid function name format") {
			found = true
			break
		}
	}
	assert.True(t, found, "Should contain invalid name format error")
}

func TestValidator_ValidateVectorTypes(t *testing.T) {
	input := `---functions---
test.get#a677244f ids:Vector<int> = vector<int>;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	validator := NewValidator(schema)
	err = validator.Validate()
	assert.NoError(t, err)
}

func TestValidator_ValidateUndefinedVectorElementType(t *testing.T) {
	input := `---functions---
test.get#a677244f ids:Vector<UndefinedType> = vector<int>;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	validator := NewValidator(schema)
	err = validator.Validate()
	assert.Error(t, err)

	errors := validator.Errors()
	// Should have undefined type error
	found := false
	for _, e := range errors {
		if strings.Contains(e.Message, "undefined type UndefinedType") {
			found = true
			break
		}
	}
	assert.True(t, found, "Should contain undefined type error")
}

func TestValidator_MultipleErrors(t *testing.T) {
	input := `---types---
user1#12345678 id:UndefinedType = User;
user2#12345678 name:string = User;

---functions---
auth.sendCode#abcdef12 param:AnotherUndefined = auth.SentCode;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	validator := NewValidator(schema)
	err = validator.Validate()
	assert.Error(t, err)

	errors := validator.Errors()
	assert.True(t, len(errors) >= 3) // Should have multiple errors

	// Check that we get different types of errors
	errorMessages := make([]string, len(errors))
	for i, e := range errors {
		errorMessages[i] = e.Message
	}

	assert.Contains(t, strings.Join(errorMessages, " "), "duplicate constructor ID")
	assert.Contains(t, strings.Join(errorMessages, " "), "undefined type")
}

func TestValidator_ComplexSchemaValidation(t *testing.T) {
	// Test a more complex schema similar to real Telegram schema
	input := `---types---
boolFalse#bc799737 = Bool;
boolTrue#997275b5 = Bool;
user#8f97c628 flags:# id:long first_name:flags.0?string last_name:flags.1?string = User;
userEmpty#d3bc4b7c = User;
sentCode#5e002502 type:string phone_code_hash:string = auth.SentCode;
authorization#33fb7bb8 user:User = auth.Authorization;

---functions---
auth.sendCode#a677244f phone_number:string api_id:int api_hash:string = auth.SentCode;
auth.signUp#80eee427 phone_number:string phone_code_hash:string first_name:string last_name:string = auth.Authorization;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	validator := NewValidator(schema)
	err = validator.Validate()
	assert.NoError(t, err)
}

func TestValidator_NamespacedTypes(t *testing.T) {
	input := `---functions---
auth.sendCode#a677244f result:auth.SentCode = mtproto.Result;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	validator := NewValidator(schema)
	err = validator.Validate()
	assert.Error(t, err) // auth.SentCode and mtproto.Result are not defined

	errors := validator.Errors()
	assert.Len(t, errors, 2)
	assert.Contains(t, errors[0].Message, "undefined type")
	assert.Contains(t, errors[1].Message, "undefined type")
}

// Test helper functions
func TestSplitNamespace(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"simple", []string{"simple"}},
		{"namespace.name", []string{"namespace", "name"}},
		{"deep.nested.name", []string{"deep", "nested", "name"}},
		{"name.with.dots", []string{"name", "with", "dots"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := splitNamespace(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsBuiltinType(t *testing.T) {
	assert.True(t, naming.IsBuiltinType("int"))
	assert.True(t, naming.IsBuiltinType("string"))
	assert.True(t, naming.IsBuiltinType("Bool"))
	assert.True(t, naming.IsBuiltinType("#"))

	assert.False(t, naming.IsBuiltinType("User"))
	assert.False(t, naming.IsBuiltinType("Message"))
	assert.False(t, naming.IsBuiltinType(""))
}

func TestGetBuiltinTypes(t *testing.T) {
	builtins := naming.GetBuiltinTypes()
	assert.Contains(t, builtins, "int")
	assert.Contains(t, builtins, "string")
	assert.Contains(t, builtins, "Bool")
	assert.Contains(t, builtins, "#")
	assert.Contains(t, builtins, "Vector")
	assert.Contains(t, builtins, "vector")
	// Length may vary as built-in types are added
}
