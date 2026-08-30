package generator

import (
	"io"
	"sort"
	"text/template"

	"github.com/r6m/tlrpc/internal/naming"
	"github.com/r6m/tlrpc/internal/parser"
)

// CodecGenerator emits static constructor maps.
type CodecGenerator struct {
	namer *naming.Namer
	out   io.Writer
}

// NewCodecGenerator creates a new CodecGenerator.
func NewCodecGenerator(namer *naming.Namer, out io.Writer) *CodecGenerator {
	return &CodecGenerator{namer: namer, out: out}
}

// CodecTemplateData holds data for codec template
type CodecTemplateData struct {
	BaseConstructors      []BaseConstructorTemplateData
	GeneratedConstructors []GeneratedConstructorTemplateData
	MethodConstructors    []MethodConstructorTemplateData
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
func (g *CodecGenerator) Generate(schema *parser.Schema) error {
	return g.GenerateStatic(schema)
}

// GenerateStatic emits a static constructor map instead of registry calls.
func (g *CodecGenerator) GenerateStatic(schema *parser.Schema) error {
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
	emittedTypeByCtor := make(map[string]string)
	for _, decl := range schema.Types {
		if len(decl.Constructors) == 1 {
			only := decl.Constructors[0]
			if g.isBaseConstructor(only.Name) {
				continue
			}
			emittedTypeByCtor[only.Name] = g.namer.TypeName(decl.Name)
			continue
		}
		for _, ctor := range decl.Constructors {
			if g.isBaseConstructor(ctor.Name) {
				continue
			}
			emittedTypeByCtor[ctor.Name] = g.namer.ConstructorName(ctor.Name)
		}
	}
	constructors := make([]parser.Constructor, 0, len(schema.Constructors))
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
		name, ok := emittedTypeByCtor[ctor.Name]
		if !ok {
			continue
		}
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
			if fn.IsTemplate || fn.IsHelper {
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

	return nil
}
