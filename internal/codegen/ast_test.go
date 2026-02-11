package codegen

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTypeRef_String(t *testing.T) {
	tests := []struct {
		name     string
		typeRef  TypeRef
		expected string
	}{
		{
			name:     "simple type",
			typeRef:  NewTypeRef("int"),
			expected: "int",
		},
		{
			name:     "namespaced type",
			typeRef:  NewNamespacedTypeRef("auth", "SentCode"),
			expected: "auth.SentCode",
		},
		{
			name:     "vector type",
			typeRef:  NewVectorTypeRef(NewTypeRef("int")),
			expected: "vector<int>",
		},
		{
			name:     "bare type",
			typeRef:  NewBareTypeRef("User"),
			expected: "!User",
		},
		{
			name:     "optional type",
			typeRef:  NewOptionalTypeRef(NewTypeRef("string")),
			expected: "string?",
		},
		{
			name: "complex type",
			typeRef: TypeRef{
				Name:      "User",
				Namespace: "mtproto",
				IsVector:  true,
				IsBare:    true,
				Generic:   &TypeRef{Name: "int"},
				Optional:  true,
			},
			expected: "!mtproto.User<int>?",
		},
		{
			name: "type with generic arg",
			typeRef: TypeRef{
				Name:       "Vector",
				GenericArg: "t",
				IsTypeVar:  true,
			},
			expected: "Vector t",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.typeRef.String())
		})
	}
}

func TestTypeRef_FullName(t *testing.T) {
	tests := []struct {
		name     string
		typeRef  TypeRef
		expected string
	}{
		{
			name:     "simple type",
			typeRef:  NewTypeRef("int"),
			expected: "int",
		},
		{
			name:     "namespaced type",
			typeRef:  NewNamespacedTypeRef("auth", "SentCode"),
			expected: "auth.SentCode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.typeRef.FullName())
		})
	}
}

func TestTypeRef_IsBuiltin(t *testing.T) {
	builtinTypes := []string{
		"int", "long", "int128", "int256", "double", "string", "bytes",
		"bool", "true", "false", "Bool", "Object", "Function", "Type", "#",
	}

	for _, typ := range builtinTypes {
		t.Run(typ, func(t *testing.T) {
			typeRef := NewTypeRef(typ)
			assert.True(t, typeRef.IsBuiltin(), "%s should be builtin", typ)
		})
	}

	nonBuiltinTypes := []string{"User", "Message", "auth.SentCode"}
	for _, typ := range nonBuiltinTypes {
		t.Run(typ, func(t *testing.T) {
			typeRef := NewTypeRef(typ)
			assert.False(t, typeRef.IsBuiltin(), "%s should not be builtin", typ)
		})
	}
}

func TestSchema_AddType(t *testing.T) {
	schema := NewSchema(222)

	userType := TypeDecl{
		Name: "User",
		Constructors: []Constructor{
			{
				Name: "user",
				ID:   0x8f97c628,
				Params: []Parameter{
					{Name: "id", Type: NewTypeRef("long")},
					{Name: "first_name", Type: NewOptionalTypeRef(NewTypeRef("string"))},
				},
				ResultType: NewTypeRef("User"),
			},
		},
		IsUnion: false,
	}

	schema.AddType(userType)

	assert.Len(t, schema.Types, 1)
	assert.Len(t, schema.Constructors, 1)
	assert.Equal(t, "User", schema.Types[0].Name)
	assert.Equal(t, "user", schema.Constructors[0].Name)
}

func TestSchema_AddFunction(t *testing.T) {
	schema := NewSchema(222)

	sendCodeFunc := FuncDecl{
		Name: "auth.sendCode",
		ID:   0xa677244f,
		Params: []Parameter{
			{Name: "phone_number", Type: NewTypeRef("string")},
			{Name: "api_id", Type: NewTypeRef("int")},
		},
		ResultType: NewNamespacedTypeRef("auth", "SentCode"),
	}

	schema.AddFunction(sendCodeFunc)

	assert.Len(t, schema.Functions, 1)
	assert.Equal(t, "auth.sendCode", schema.Functions[0].Name)
}

func TestSchema_FindType(t *testing.T) {
	schema := NewSchema(222)

	userType := TypeDecl{Name: "User"}
	schema.AddType(userType)

	found, ok := schema.FindType("User")
	assert.True(t, ok)
	assert.Equal(t, "User", found.Name)

	_, ok = schema.FindType("NonExistent")
	assert.False(t, ok)
}

func TestSchema_FindConstructor(t *testing.T) {
	schema := NewSchema(222)

	ctor := Constructor{Name: "user", ID: 0x8f97c628}
	userType := TypeDecl{Name: "User", Constructors: []Constructor{ctor}}
	schema.AddType(userType)

	found, ok := schema.FindConstructor(0x8f97c628)
	assert.True(t, ok)
	assert.Equal(t, "user", found.Name)

	_, ok = schema.FindConstructor(0x12345678)
	assert.False(t, ok)
}

func TestSchema_FindFunction(t *testing.T) {
	schema := NewSchema(222)

	fn := FuncDecl{Name: "auth.sendCode", ID: 0xa677244f}
	schema.AddFunction(fn)

	found, ok := schema.FindFunction(0xa677244f)
	assert.True(t, ok)
	assert.Equal(t, "auth.sendCode", found.Name)

	_, ok = schema.FindFunction(0x12345678)
	assert.False(t, ok)
}

func TestSchema_String(t *testing.T) {
	schema := NewSchema(222)

	userType := TypeDecl{
		Name: "User",
		Constructors: []Constructor{
			{Name: "user", ID: 0x8f97c628, ResultType: NewTypeRef("User")},
		},
	}

	sendCodeFunc := FuncDecl{
		Name:       "auth.sendCode",
		ID:         0xa677244f,
		ResultType: NewNamespacedTypeRef("auth", "SentCode"),
	}

	schema.AddType(userType)
	schema.AddFunction(sendCodeFunc)

	str := schema.String()
	assert.Contains(t, str, "Schema{Layer: 222")
	assert.Contains(t, str, "Types: [")
	assert.Contains(t, str, "Functions: [")
	assert.Contains(t, str, "auth.sendCode")
}

func TestConstructor_String(t *testing.T) {
	ctor := Constructor{
		Name:   "user",
		ID:     0x8f97c628,
		IsBare: true,
		Params: []Parameter{
			{Name: "id", Type: NewTypeRef("long")},
			{Name: "first_name", Type: NewOptionalTypeRef(NewTypeRef("string")), FlagBit: intPtr(0)},
		},
		ResultType: NewTypeRef("User"),
	}

	str := ctor.String()
	assert.Contains(t, str, "%Constructor{Name: \"user\", ID: 0x8f97c628")
	assert.Contains(t, str, "Params: [")
	assert.Contains(t, str, "ResultType: User")
}

func TestFuncDecl_String(t *testing.T) {
	fn := FuncDecl{
		Name: "auth.sendCode",
		ID:   0xa677244f,
		Params: []Parameter{
			{Name: "phone_number", Type: NewTypeRef("string")},
		},
		ResultType: NewNamespacedTypeRef("auth", "SentCode"),
	}

	str := fn.String()
	assert.Contains(t, str, "FuncDecl{Name: \"auth.sendCode\", ID: 0xa677244f")
	assert.Contains(t, str, "Params: [")
	assert.Contains(t, str, "ResultType: auth.SentCode")
}

func TestParameter_String(t *testing.T) {
	param := Parameter{
		Name:    "first_name",
		Type:    NewOptionalTypeRef(NewTypeRef("string")),
		FlagBit: intPtr(0),
	}

	str := param.String()
	assert.Contains(t, str, "Parameter{Name: \"first_name\"")
	assert.Contains(t, str, "Type: string?")
	assert.Contains(t, str, "FlagBit: 0")

	paramNoFlag := Parameter{
		Name: "id",
		Type: NewTypeRef("long"),
	}

	str = paramNoFlag.String()
	assert.Contains(t, str, "Parameter{Name: \"id\"")
	assert.Contains(t, str, "Type: long")
	assert.NotContains(t, str, "FlagBit")
}

func TestGenericParam_String(t *testing.T) {
	param := GenericParam{
		Name:       "t",
		Constraint: "Type",
		Pos:        Position{Line: 1, Column: 5},
	}

	assert.Equal(t, "t:Type", param.String())
}

func TestIsTypeVariable(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"lowercase single letter", "t", true},
		{"uppercase single letter", "T", true},
		{"multiple letters", "Type", false},
		{"empty string", "", false},
		{"number", "1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isTypeVariable(tt.input))
		})
	}
}

// Helper function to create int pointer for tests
func intPtr(i int) *int {
	return &i
}
