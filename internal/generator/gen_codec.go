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
	IsLayered              bool
	BaseLayer              int
	BaseConstructors       []BaseConstructorTemplateData
	StaticConstructors     []GeneratedConstructorTemplateData
	ConstructorLayerGroups []ConstructorLayerGroupTemplateData
	StaticMethods          []MethodConstructorTemplateData
	MethodLayerGroups      []MethodLayerGroupTemplateData
}

type ConstructorLayerGroupTemplateData struct {
	ID       uint32
	Variants []GeneratedConstructorTemplateData
}

type MethodLayerGroupTemplateData struct {
	ID       uint32
	Variants []MethodConstructorTemplateData
}

// BaseConstructorTemplateData holds data for base constructor entries
type BaseConstructorTemplateData struct {
	ID   uint32
	Code string
}

// GeneratedConstructorTemplateData holds data for generated constructor entries
type GeneratedConstructorTemplateData struct {
	ID       uint32
	Name     string
	MinLayer int
	MaxLayer int
}

// MethodConstructorTemplateData holds data for method constructor entries
type MethodConstructorTemplateData struct {
	ID       uint32
	Name     string
	Type     string
	MinLayer int
	MaxLayer int
}

// codecTemplate generates static constructor and method maps
const codecTemplate = `// Static constructor map for efficient decoding
{{if .IsLayered}}
func tlLayerSupports(layer, minLayer, maxLayer int) bool {
	if layer == 0 {
		layer = {{.BaseLayer}}
	}
	return (minLayer == 0 || layer >= minLayer) && (maxLayer == 0 || layer <= maxLayer)
}
{{end}}

var staticConstructors = map[uint32]func() tlrpc.TLObject{
	// Base MTProto types
{{- range .BaseConstructors}}
	{{hex .ID}}: {{.Code}},
{{- end}}

{{- if .StaticConstructors}}
	// Generated types
{{- range .StaticConstructors}}
	{{hex .ID}}: func() tlrpc.TLObject { return &{{.Name}}{} },
{{- end}}
{{- end}}
}

// GetStaticConstructors returns the static constructor map for codec initialization
func GetStaticConstructors() map[uint32]func() tlrpc.TLObject {
	return staticConstructors
}

type tlConstructorLayerVariant struct {
	minLayer int
	maxLayer int
	newObject func() tlrpc.TLObject
}

var constructorLayerVariants = map[uint32][]tlConstructorLayerVariant{
{{- range .ConstructorLayerGroups}}
	{{hex .ID}}: {
	{{- range .Variants}}
		{minLayer: {{.MinLayer}}, maxLayer: {{.MaxLayer}}, newObject: func() tlrpc.TLObject { return &{{.Name}}{} }},
	{{- end}}
	},
{{- end}}
}

// NewConstructorForLayer constructs the wire shape selected by an incoming
// constructor ID and the negotiated session layer.
func NewConstructorForLayer(id uint32, layer int) (tlrpc.TLObject, bool) {
	variants, hasLayerVariants := constructorLayerVariants[id]
	for _, variant := range variants {
		if layer == 0 || ((variant.minLayer == 0 || layer >= variant.minLayer) && (variant.maxLayer == 0 || layer <= variant.maxLayer)) {
			return variant.newObject(), true
		}
	}
	if hasLayerVariants {
		return nil, false
	}
	constructor, ok := staticConstructors[id]
	if !ok {
		return nil, false
	}
	return constructor(), true
}

// Static method constructor map for RPC request deserialization
var staticMethods = map[string]func() tlrpc.TLObject{
{{- range .StaticMethods}}
	{{quote .Name}}: func() tlrpc.TLObject { return &{{.Type}}{} },
{{- end}}
}

// GetStaticMethods returns the static method constructor map
func GetStaticMethods() map[string]func() tlrpc.TLObject {
	return staticMethods
}

var methodLayerVariants = map[uint32][]tlConstructorLayerVariant{
{{- range .MethodLayerGroups}}
	{{hex .ID}}: {
	{{- range .Variants}}
		{minLayer: {{.MinLayer}}, maxLayer: {{.MaxLayer}}, newObject: func() tlrpc.TLObject { return &{{.Type}}{} }},
	{{- end}}
	},
{{- end}}
}

// NewMethodRequestForLayer constructs a typed request wire variant.
func NewMethodRequestForLayer(id uint32, layer int) (tlrpc.TLObject, bool) {
	for _, variant := range methodLayerVariants[id] {
		if layer == 0 || ((variant.minLayer == 0 || layer >= variant.minLayer) && (variant.maxLayer == 0 || layer <= variant.maxLayer)) {
			return variant.newObject(), true
		}
	}
	return nil, false
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
			emittedTypeByCtor[variantKey(only.Name, only.VariantLayer)] = typeName(g.namer, decl.Name, decl.VariantLayer)
			continue
		}
		for _, ctor := range decl.Constructors {
			if g.isBaseConstructor(ctor.Name) {
				continue
			}
			emittedTypeByCtor[variantKey(ctor.Name, ctor.VariantLayer)] = constructorName(g.namer, ctor)
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
		if constructors[i].ID == constructors[j].ID {
			return constructors[i].MinLayer < constructors[j].MinLayer
		}
		return constructors[i].ID < constructors[j].ID
	})

	for _, ctor := range constructors {
		name, ok := emittedTypeByCtor[variantKey(ctor.Name, ctor.VariantLayer)]
		if !ok {
			continue
		}
		generatedConstructors = append(generatedConstructors, GeneratedConstructorTemplateData{
			ID: ctor.ID, Name: name, MinLayer: ctor.MinLayer, MaxLayer: ctor.MaxLayer,
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
			requestType := requestName(g.namer, fn)
			methodConstructors = append(methodConstructors, MethodConstructorTemplateData{
				ID: fn.ID, Name: methodName, Type: requestType, MinLayer: fn.MinLayer, MaxLayer: fn.MaxLayer,
			})
		}
	}

	constructorGroups := groupConstructorVariants(generatedConstructors)
	methodGroups := groupMethodVariants(methodConstructors)
	data := CodecTemplateData{
		IsLayered:              schema.IsLayered,
		BaseLayer:              schema.BaseLayer,
		BaseConstructors:       baseConstructors,
		StaticConstructors:     firstConstructors(constructorGroups),
		ConstructorLayerGroups: constructorGroups,
		StaticMethods:          firstMethodsByName(methodConstructors),
		MethodLayerGroups:      methodGroups,
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

func groupConstructorVariants(variants []GeneratedConstructorTemplateData) []ConstructorLayerGroupTemplateData {
	var groups []ConstructorLayerGroupTemplateData
	for _, variant := range variants {
		if len(groups) == 0 || groups[len(groups)-1].ID != variant.ID {
			groups = append(groups, ConstructorLayerGroupTemplateData{ID: variant.ID})
		}
		groups[len(groups)-1].Variants = append(groups[len(groups)-1].Variants, variant)
	}
	return groups
}

func firstConstructors(groups []ConstructorLayerGroupTemplateData) []GeneratedConstructorTemplateData {
	out := make([]GeneratedConstructorTemplateData, 0, len(groups))
	for _, group := range groups {
		if len(group.Variants) > 0 {
			out = append(out, group.Variants[0])
		}
	}
	return out
}

func groupMethodVariants(variants []MethodConstructorTemplateData) []MethodLayerGroupTemplateData {
	sort.SliceStable(variants, func(i, j int) bool {
		if variants[i].ID == variants[j].ID {
			return variants[i].MinLayer < variants[j].MinLayer
		}
		return variants[i].ID < variants[j].ID
	})
	var groups []MethodLayerGroupTemplateData
	for _, variant := range variants {
		if len(groups) == 0 || groups[len(groups)-1].ID != variant.ID {
			groups = append(groups, MethodLayerGroupTemplateData{ID: variant.ID})
		}
		groups[len(groups)-1].Variants = append(groups[len(groups)-1].Variants, variant)
	}
	return groups
}

func firstMethodsByName(variants []MethodConstructorTemplateData) []MethodConstructorTemplateData {
	seen := make(map[string]struct{}, len(variants))
	var out []MethodConstructorTemplateData
	for _, variant := range variants {
		if _, exists := seen[variant.Name]; exists {
			continue
		}
		seen[variant.Name] = struct{}{}
		out = append(out, variant)
	}
	return out
}
