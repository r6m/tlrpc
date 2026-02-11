package codegen

// Built-in types in TL schema
var builtinTypes = map[string]bool{
	// Basic types
	"int":    true,
	"long":   true,
	"int128": true,
	"int256": true,
	"double": true,
	"string": true,
	"bytes":  true,

	// Boolean types
	"bool":  true,
	"true":  true,
	"false": true,
	"Bool":  true,

	// Special types
	"Object":   true,
	"Function": true,
	"Type":     true,
	"#":        true, // flags type

	// Vector type (generic)
	"Vector": true,
	"vector": true,
}

// IsBuiltinType checks if a type name is a built-in TL type
func IsBuiltinType(name string) bool {
	return builtinTypes[name]
}

// GetBuiltinTypes returns a slice of all built-in type names
func GetBuiltinTypes() []string {
	var types []string
	for typ := range builtinTypes {
		types = append(types, typ)
	}
	return types
}
