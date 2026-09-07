package generator

import (
	"fmt"
	"io"
	"strings"

	"github.com/r6m/tlrpc/internal/naming"
	"github.com/r6m/tlrpc/internal/parser"
)

func writeKnownFlagValidation(out io.Writer, parameters []parser.Parameter, setName, valueName, layerName, indent string) error {
	if _, err := fmt.Fprintf(out, "%sknownFlags := uint32(0)\n", indent); err != nil {
		return err
	}
	seenBits := make(map[int]struct{})
	for _, parameter := range parameters {
		bit := flagBit(parameter)
		if bit == nil || flagSetName(parameter) != setName {
			continue
		}
		if _, exists := seenBits[*bit]; exists {
			continue
		}
		seenBits[*bit] = struct{}{}
		if parameter.MinLayer != 0 || parameter.MaxLayer != 0 {
			if _, err := fmt.Fprintf(out, "%sif tlLayerSupports(%s, %d, %d) {\n%s\tknownFlags |= 1 << %d\n%s}\n", indent, layerName, parameter.MinLayer, parameter.MaxLayer, indent, *bit, indent); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(out, "%sknownFlags |= 1 << %d\n", indent, *bit); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(out, "%sif unknownFlags := %s &^ knownFlags; unknownFlags != 0 {\n%s\treturn fmt.Errorf(\"unknown %s bits for layer %%d: 0x%%08x\", %s, unknownFlags)\n%s}\n", indent, valueName, indent, setName, layerName, indent)
	return err
}

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
		if decl.Name != name || decl.VariantLayer != t.VariantLayer {
			continue
		}
		if decl.IsUnion || len(decl.Constructors) > 1 {
			return true
		}
		return false
	}
	return false
}

func variantSuffix(layer int) string {
	if layer == 0 {
		return ""
	}
	return fmt.Sprintf("Layer%d", layer)
}

func variantKey(name string, layer int) string {
	return fmt.Sprintf("%s@%d", name, layer)
}

func typeName(namer *naming.Namer, name string, layer int) string {
	return namer.TypeName(name) + variantSuffix(layer)
}

func constructorName(namer *naming.Namer, constructor parser.Constructor) string {
	return namer.ConstructorName(constructor.Name) + variantSuffix(constructor.VariantLayer)
}

func requestName(namer *naming.Namer, function parser.FuncDecl) string {
	return namer.RequestName(function.Name) + variantSuffix(function.VariantLayer)
}

func methodName(namer *naming.Namer, function parser.FuncDecl) string {
	return namer.MethodName(function.Name) + variantSuffix(function.VariantLayer)
}

func unionInterfaceName(namer *naming.Namer, t parser.TypeRef) string {
	if t.Namespace != "" {
		return typeName(namer, t.Namespace+"."+t.Name, t.VariantLayer) + "Type"
	}
	return typeName(namer, t.Name, t.VariantLayer) + "Type"
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
