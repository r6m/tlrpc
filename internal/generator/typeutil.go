package generator

import (
	"github.com/r6m/tlrpc/internal/naming"
	"github.com/r6m/tlrpc/internal/parser"
	"strings"
)

func isUnionType(schema *parser.Schema, t parser.TypeRef) bool {
	if schema == nil {
		return false
	}
	name := t.Name
	if t.Namespace != "" {
		name = t.Namespace + "." + t.Name
	}
	if naming.IsBuiltinType(name) || naming.IsBuiltinType(t.Name) {
		return false
	}
	if schema.UnionTypes != nil && schema.UnionTypes[name] {
		return true
	}
	for i := range schema.Types {
		decl := schema.Types[i]
		if decl.Name != name {
			continue
		}
		if decl.IsUnion || len(decl.Constructors) > 1 {
			return true
		}
		return false
	}
	return false
}

func unionInterfaceName(namer *naming.Namer, t parser.TypeRef) string {
	if t.Namespace != "" {
		return namer.TypeName(t.Namespace+"."+t.Name) + "Type"
	}
	return namer.TypeName(t.Name) + "Type"
}

func needsOptionalPointer(t parser.TypeRef, base string, schema *parser.Schema) bool {
	if !t.Optional || isTrueType(t) {
		return false
	}
	if isUnionType(schema, t) || strings.HasPrefix(base, "[]") {
		return false
	}
	return true
}

func isBuiltinTLType(name string) bool {
	switch name {
	case "int", "long", "int128", "int256", "double", "string", "bytes", "Bool", "bool", "true", "false", "#", "error", "null", "vector":
		return true
	default:
		return false
	}
}

func shouldUsePointerForType(schema *parser.Schema, t parser.TypeRef) bool {
	if t.IsVector {
		return false
	}
	if isUnionType(schema, t) {
		return false
	}
	if isBuiltinTLType(t.Name) {
		return false
	}
	return true
}
