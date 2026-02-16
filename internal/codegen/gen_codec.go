package codegen

import (
	"fmt"
	"io"
	"sort"
	"text/template"
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

// CodecTemplateData holds data for codec template
type CodecTemplateData struct {
	BaseConstructors    []BaseConstructorTemplateData
	GeneratedConstructors []GeneratedConstructorTemplateData
	MethodConstructors   []MethodConstructorTemplateData
}

// BaseConstructorTemplateData holds data for base constructor entries
type BaseConstructorTemplateData struct {
	ID   uint32
	Code string
}

// GeneratedConstructorTemplateData holds data for generated constructor entries
type GeneratedConstructorTemplateData struct {
	ID   uint32
	Name string
}

// MethodConstructorTemplateData holds data for method constructor entries
type MethodConstructorTemplateData struct {
	Name string
	Type string
}

// codecTemplate generates static constructor and method maps
const codecTemplate = `// Static constructor map for efficient decoding
var staticConstructors = map[uint32]func() tlrpc.TLObject{
	// Base MTProto types
{{- range .BaseConstructors}}
	{{hex .ID}}: {{.Code}},
{{- end}}

{{- if .GeneratedConstructors}}
	// Generated types
{{- range .GeneratedConstructors}}
	{{hex .ID}}: func() tlrpc.TLObject { return &{{.Name}}{} },
{{- end}}
{{- end}}
}

// GetStaticConstructors returns the static constructor map for codec initialization
func GetStaticConstructors() map[uint32]func() tlrpc.TLObject {
	return staticConstructors
}

// Static method constructor map for RPC request deserialization
var staticMethods = map[string]func() tlrpc.TLObject{
{{- range .MethodConstructors}}
	{{quote .Name}}: func() tlrpc.TLObject { return &{{.Type}}{} },
{{- end}}
}

// GetStaticMethods returns the static method constructor map
func GetStaticMethods() map[string]func() tlrpc.TLObject {
	return staticMethods
}
`

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
	// Build base constructors data
	baseConstructors := []BaseConstructorTemplateData{
		{ID: 0x3fedd339, Code: "func() tlrpc.TLObject { return &types.True{} }"},
		{ID: 0xc4b9f9bb, Code: "func() tlrpc.TLObject { return &types.Error{} }"},
		{ID: 0x56730bcc, Code: "func() tlrpc.TLObject { return &types.Null{} }"},
		{ID: 0xb5286e24, Code: "func() tlrpc.TLObject { s := types.String(\"\"); return &s }"},
		{ID: 0x0a1cdbd1, Code: "func() tlrpc.TLObject { return &types.Bytes{} }"},
		{ID: 0x84c1e679, Code: "func() tlrpc.TLObject { return &types.Int128{} }"},
		{ID: 0x7bed4774, Code: "func() tlrpc.TLObject { return &types.Int256{} }"},
		{ID: 0x2210c154, Code: "func() tlrpc.TLObject { d := types.Double(0); return &d }"},
	}

	// Build generated constructors data
	var generatedConstructors []GeneratedConstructorTemplateData
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

	for _, ctor := range constructors {
		name := g.namer.ConstructorName(ctor.Name)
		generatedConstructors = append(generatedConstructors, GeneratedConstructorTemplateData{
			ID:   ctor.ID,
			Name: name,
		})
	}

	// Build method constructors data
	var methodConstructors []MethodConstructorTemplateData
	services := groupByService(schema.Functions)
	serviceNames := sortedKeys(services)
	for _, service := range serviceNames {
		for _, fn := range services[service] {
			if fn.IsTemplate {
				continue
			}
			methodName := fn.Name // Use full method name with service prefix
			requestName := g.namer.RequestName(fn.Name)
			methodConstructors = append(methodConstructors, MethodConstructorTemplateData{
				Name: methodName,
				Type: requestName,
			})
		}
	}

	data := CodecTemplateData{
		BaseConstructors:      baseConstructors,
		GeneratedConstructors: generatedConstructors,
		MethodConstructors:    methodConstructors,
	}

	tmpl, err := template.New("codec").Funcs(templateFuncMap()).Parse(codecTemplate)
	if err != nil {
		return err
	}

	if err := tmpl.Execute(g.out, data); err != nil {
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
