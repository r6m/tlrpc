package codegen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParser_ParseEmpty(t *testing.T) {
	parser := NewParser("")
	schema, err := parser.Parse()
	require.NoError(t, err)
	assert.NotNil(t, schema)
	assert.Len(t, schema.Types, 0)
	assert.Len(t, schema.Functions, 0)
}

func TestParser_ParseSimpleConstructor(t *testing.T) {
	input := `---types---
userEmpty#d3bc4b7c = User;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	assert.Len(t, schema.Types, 1)
	assert.Len(t, schema.Functions, 0)

	userType := schema.Types[0]
	assert.Equal(t, "User", userType.Name)
	assert.Len(t, userType.Constructors, 1)

	ctor := userType.Constructors[0]
	assert.Equal(t, "userEmpty", ctor.Name)
	assert.Equal(t, uint32(0xd3bc4b7c), ctor.ID)
	assert.Len(t, ctor.Params, 0)
	assert.Equal(t, "User", ctor.ResultType.Name)
}

func TestParser_ParseConstructorWithParams(t *testing.T) {
	input := `---types---
user#8f97c628 flags:# id:long first_name:flags.0?string last_name:flags.1?string = User;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	assert.Len(t, schema.Types, 1)
	userType := schema.Types[0]
	assert.Equal(t, "User", userType.Name)
	assert.Len(t, userType.Constructors, 1)

	ctor := userType.Constructors[0]
	assert.Equal(t, "user", ctor.Name)
	assert.Equal(t, uint32(0x8f97c628), ctor.ID)
	assert.Len(t, ctor.Params, 4)

	// Check flags parameter
	flagsParam := ctor.Params[0]
	assert.Equal(t, "flags", flagsParam.Name)
	assert.Equal(t, "#", flagsParam.Type.Name)

	// Check id parameter
	idParam := ctor.Params[1]
	assert.Equal(t, "id", idParam.Name)
	assert.Equal(t, "long", idParam.Type.Name)

	// Check conditional parameters
	firstNameParam := ctor.Params[2]
	assert.Equal(t, "first_name", firstNameParam.Name)
	assert.Equal(t, "string", firstNameParam.Type.Name)
	assert.True(t, firstNameParam.Type.Optional)
	assert.NotNil(t, firstNameParam.FlagBit)
	assert.Equal(t, 0, *firstNameParam.FlagBit)
}

func TestParser_ParseBareConstructor(t *testing.T) {
	input := `---types---
%userEmpty#d3bc4b7c = User;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	ctor := schema.Constructors[0]
	assert.True(t, ctor.IsBare)
	assert.Equal(t, "userEmpty", ctor.Name)
}

func TestParser_ParseFunction(t *testing.T) {
	input := `---functions---
auth.sendCode#a677244f phone_number:string api_id:int api_hash:string = auth.SentCode;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	assert.Len(t, schema.Functions, 1)
	fn := schema.Functions[0]
	assert.Equal(t, "auth.sendCode", fn.Name)
	assert.Equal(t, uint32(0xa677244f), fn.ID)
	assert.Len(t, fn.Params, 3)
	assert.Equal(t, "auth.SentCode", fn.ResultType.FullName())
}

func TestParser_ParseMultipleSections(t *testing.T) {
	input := `---types---
userEmpty#d3bc4b7c = User;
user#8f97c628 id:long = User;

---functions---
auth.sendCode#a677244f phone_number:string = auth.SentCode;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	assert.Len(t, schema.Types, 1)
	assert.Len(t, schema.Functions, 1)
	assert.Len(t, schema.Constructors, 2)

	userType := schema.Types[0]
	assert.True(t, userType.IsUnion)
	assert.Len(t, userType.Constructors, 2)
}

func TestParser_ParseVectorType(t *testing.T) {
	input := `---functions---
messages.getMessages#123 ids:vector<long> = messages.Messages;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	fn := schema.Functions[0]
	idsParam := fn.Params[0]
	assert.Equal(t, "ids", idsParam.Name)
	assert.True(t, idsParam.Type.IsVector)
	assert.Equal(t, "long", idsParam.Type.Generic.Name)
}

func TestParser_ParseNamespacedTypes(t *testing.T) {
	input := `---functions---
auth.sendCode#a677244f result:auth.SentCode = mtproto.Result;`

	parser := NewParser(input)
	schema, err := parser.Parse()
	require.NoError(t, err)

	fn := schema.Functions[0]
	resultParam := fn.Params[0]
	assert.Equal(t, "auth", resultParam.Type.Namespace)
	assert.Equal(t, "SentCode", resultParam.Type.Name)
	assert.Equal(t, "auth.SentCode", resultParam.Type.FullName())
}

func TestParser_ParseHexIDs(t *testing.T) {
	tests := []struct {
		input    string
		expected uint32
	}{
		{"user#8f97c628", 0x8f97c628},
		{"test#0x123abc", 0x123abc},
		{"func#a677244f", 0xa677244f},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			fullInput := "---types---\n" + tt.input + " = Test;"
			parser := NewParser(fullInput)
			schema, err := parser.Parse()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, schema.Constructors[0].ID)
		})
	}
}

func TestParser_ErrorHandling(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		errMsg   string
	}{
		{
			name:   "missing hash",
			input:  "---types---\nuser = User;",
			errMsg: "expected # after constructor name",
		},
		{
			name:   "invalid hex",
			input:  "---types---\nuser#gggg = User;",
			errMsg: "parse error",
		},
		{
			name:   "missing equals",
			input:  "---types---\nuser#123 User;",
			errMsg: "expected : in parameter",
		},
		{
			name:   "unexpected token",
			input:  "---types---\n123 = User;",
			errMsg: "expected identifier",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser(tt.input)
			_, err := parser.Parse()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

// func TestParser_CRC32Computation(t *testing.T) {
// 	// Test CRC32 computation for constructors without explicit IDs
// 	input := `---types---
// user flags:# id:long = User;`

// 	parser := NewParser(input)
// 	schema, err := parser.Parse()
// 	require.NoError(t, err)

// 	// The ID should be computed as CRC32 of the serialization format
// 	ctor := schema.Constructors[0]
// 	assert.NotEqual(t, uint32(0), ctor.ID) // Should have computed an ID
// }

// Benchmark parsing performance
// func BenchmarkParser_Parse(b *testing.B) {
// 	input := `---types---
// user#8f97c628 flags:# id:long first_name:flags.0?string last_name:flags.1?string username:flags.2?string phone:flags.3?string photo:flags.4?UserProfilePhoto access_hash:flags.5?long bot:flags.6?true restricted:flags.7?true restriction_reason:flags.7?string bot_inline_placeholder:flags.8?string = User;
// userEmpty#d3bc4b7c id:long = User;

// ---functions---
// auth.sendCode#a677244f phone_number:string api_id:int api_hash:string flags:flags current_number:flags.0?true = auth.SentCode;
// messages.getMessages#4223b72b id:Vector<InputMessage> = messages.Messages;`

// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		parser := NewParser(input)
// 		_, err := parser.Parse()
// 		if err != nil {
// 			b.Fatal(err)
// 		}
// 	}
// }