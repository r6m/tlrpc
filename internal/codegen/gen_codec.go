package codegen

import (
	"fmt"
	"io"
	"sort"
)

// CodecGenerator emits static constructor maps.
type CodecGenerator struct {
	namer *Namer
	out   io.Writer
}

// NewCodecGenerator creates a new CodecGenerator.
func NewCodecGenerator(namer *Namer, out io.Writer) *CodecGenerator {
	return &CodecGenerator{namer: namer, out: out}
}

// isBaseConstructor checks if a constructor represents a base MTProto type
func (g *CodecGenerator) isBaseConstructor(name string) bool {
	baseConstructorNames := map[string]bool{
		"boolFalse": true,
		"boolTrue":  true,
		"true":      true,
		"false":     true,
		"error":     true,
		"null":      true,
		"string":    true,
		"bytes":     true,
		"int128":    true,
		"int256":    true,
		"double":    true,
		"vector":    true,
	}

	return baseConstructorNames[name]
}

// Generate emits a static constructor map for the schema.
func (g *CodecGenerator) Generate(schema *Schema) error {
	return g.GenerateStatic(schema)
}

// GenerateStatic emits a static constructor map instead of registry calls.
func (g *CodecGenerator) GenerateStatic(schema *Schema) error {
	if _, err := io.WriteString(g.out, "// Static constructor map for efficient decoding\nvar staticConstructors = map[uint32]func() tlrpc.TLObject{\n"); err != nil {
		return err
	}

	// Include base MTProto type constructors
	if _, err := io.WriteString(g.out, "\t// Base MTProto types\n"); err != nil {
		return err
	}
	baseConstructors := []struct {
		id   uint32
		name string
	}{
		{0x3fedd339, "func() tlrpc.TLObject { return &types.True{} }"},
		{0xc4b9f9bb, "func() tlrpc.TLObject { return &types.Error{} }"},
		{0x56730bcc, "func() tlrpc.TLObject { return &types.Null{} }"},
		{0xb5286e24, "func() tlrpc.TLObject { s := types.String(\"\"); return &s }"},
		{0x0a1cdbd1, "func() tlrpc.TLObject { return &types.Bytes{} }"},
		{0x84c1e679, "func() tlrpc.TLObject { return &types.Int128{} }"},
		{0x7bed4774, "func() tlrpc.TLObject { return &types.Int256{} }"},
		{0x2210c154, "func() tlrpc.TLObject { d := types.Double(0); return &d }"},
	}

	for _, ctor := range baseConstructors {
		if _, err := fmt.Fprintf(g.out, "\t0x%08x: %s,\n", ctor.id, ctor.name); err != nil {
			return err
		}
	}

	// Add generated constructors (skip base types)
	constructors := make([]Constructor, 0, len(schema.Constructors))
	for _, ctor := range schema.Constructors {
		if len(ctor.GenericParams) > 0 || ctor.ResultType.IsTypeVar {
			continue
		}
		// Skip base types that are already included above
		if g.isBaseConstructor(ctor.Name) {
			continue
		}
		constructors = append(constructors, ctor)
	}
	sort.Slice(constructors, func(i, j int) bool {
		return constructors[i].ID < constructors[j].ID
	})

	if len(constructors) > 0 {
		if _, err := io.WriteString(g.out, "\n\t// Generated types\n"); err != nil {
			return err
		}
		for _, ctor := range constructors {
			name := g.namer.ConstructorName(ctor.Name)
			if _, err := fmt.Fprintf(g.out, "\t0x%08x: func() tlrpc.TLObject { return &%s{} },\n", ctor.ID, name); err != nil {
				return err
			}
		}
	}

	if _, err := io.WriteString(g.out, "}\n\n// GetStaticConstructors returns the static constructor map for codec initialization\nfunc GetStaticConstructors() map[uint32]func() tlrpc.TLObject {\n\treturn staticConstructors\n}\n\n// Static method constructor map for RPC request deserialization\nvar staticMethods = map[string]func() tlrpc.TLObject{\n"); err != nil {
		return err
	}

	services := groupByService(schema.Functions)
	serviceNames := sortedKeys(services)
	for _, service := range serviceNames {
		for _, fn := range services[service] {
			if fn.IsTemplate {
				continue
			}
			methodName := fn.Name // Use full method name with service prefix
			requestName := g.namer.RequestName(fn.Name)
			if _, err := fmt.Fprintf(g.out, "\t%q: func() tlrpc.TLObject { return &%s{} },\n", methodName, requestName); err != nil {
				return err
			}
		}
	}

	if _, err := io.WriteString(g.out, "}\n\n// GetStaticMethods returns the static method constructor map\nfunc GetStaticMethods() map[string]func() tlrpc.TLObject {\n\treturn staticMethods\n}\n\n"); err != nil {
		return err
	}

	// Generate method registration for RPC methods (still needed for service dispatch)
	if err := g.generateMethodRegistration(schema); err != nil {
		return err
	}

	return nil
}

// generateMethodRegistration emits method registration for RPC calls.
func (g *CodecGenerator) generateMethodRegistration(schema *Schema) error {
	if _, err := io.WriteString(g.out, "// Method registration for RPC dispatch\nfunc RegisterMethods(reg *codec.Registry) {\n\tif reg == nil {\n\t\treturn\n\t}\n"); err != nil {
		return err
	}

	services := groupByService(schema.Functions)
	serviceNames := sortedKeys(services)
	for _, service := range serviceNames {
		for _, fn := range services[service] {
			if fn.IsTemplate {
				continue
			}
			methodName := fn.Name // Use full method name with service prefix
			requestName := g.namer.RequestName(fn.Name)
			if _, err := fmt.Fprintf(g.out, "\treg.RegisterMethod(%q, func() tlrpc.TLObject { return &%s{} })\n", methodName, requestName); err != nil {
				return err
			}
		}
	}

	if _, err := io.WriteString(g.out, "}\n"); err != nil {
		return err
	}

	return nil
}
