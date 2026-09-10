package generator

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"text/template"

	"github.com/r6m/tlrpc/internal/naming"
	"github.com/r6m/tlrpc/internal/parser"
)

// TypeGenerator generates Go types from TL declarations.
type TypeGenerator struct {
	namer         *naming.Namer
	out           io.Writer
	schema        *parser.Schema
	usesBaseTypes bool
	emittedTypes  map[string]struct{}
	bareTypes     map[string]struct{}
}

// NewTypeGenerator creates a new type generator.
func NewTypeGenerator(namer *naming.Namer, out io.Writer, schema *parser.Schema) *TypeGenerator {
	g := &TypeGenerator{namer: namer, out: out, schema: schema, emittedTypes: make(map[string]struct{}), bareTypes: make(map[string]struct{})}
	if schema != nil {
		mark := func(reference parser.TypeRef) {}
		mark = func(reference parser.TypeRef) {
			if reference.IsVector && reference.Generic != nil {
				mark(*reference.Generic)
				return
			}
			if name, ok := bareConcreteTypeName(namer, schema, reference); ok {
				g.bareTypes[name] = struct{}{}
			}
		}
		for _, constructor := range schema.Constructors {
			for _, parameter := range constructor.Params {
				mark(parameter.Type)
			}
		}
		for _, function := range schema.Functions {
			for _, parameter := range function.Params {
				mark(parameter.Type)
			}
		}
	}
	return g
}

// UsesBaseTypes returns true if this generator references base MTProto types
func (g *TypeGenerator) UsesBaseTypes() bool {
	return g.usesBaseTypes
}

// templateFuncMap returns template functions for code generation
func templateFuncMap() template.FuncMap {
	return template.FuncMap{
		"hex": func(n uint32) string {
			return fmt.Sprintf("0x%08x", n)
		},
		"quote": func(s string) string {
			return fmt.Sprintf("%q", s)
		},
		"lower": strings.ToLower,
		"upper": strings.ToUpper,
	}
}

// ConstructorTemplateData holds data for constructor template
type ConstructorTemplateData struct {
	Name          string
	Fields        []FieldTemplateData
	ID            uint32
	TLName        string
	HasFlags      bool
	FlagFields    []FlagFieldTemplateData
	SerializeTL   string
	DeserializeTL string
	IsLayered     bool
}

// FieldTemplateData holds data for struct fields
type FieldTemplateData struct {
	Name string
	Type string
}

// FlagFieldTemplateData holds data for flag fields
type FlagFieldTemplateData struct {
	Condition string
	Bit       int
}

// InterfaceTemplateData holds data for interface template
type InterfaceTemplateData struct {
	Name         string
	BaseName     string
	Constructors []string
}

// interfaceTemplate generates a polymorphic interface for union types
const interfaceTemplate = `type {{.Name}} interface {
	is{{.BaseName}}Type()
	ConstructorID() uint32
	TLName() string
	SerializeTL(io.Writer) error
	DeserializeTL(io.Reader) error
}
{{range .Constructors}}
func (*{{.}}) is{{$.BaseName}}Type() {}
{{end}}
`

// constructorTemplate generates a constructor struct with its methods
const constructorTemplate = `type {{.Name}} struct {
{{- range .Fields}}
	{{.Name}} {{.Type}}
{{- end}}
}

func (v *{{.Name}}) ConstructorID() uint32 { return {{hex .ID}} }
func (v *{{.Name}}) Method() string { return "" }
func (v *{{.Name}}) TLName() string { return {{quote .TLName}} }

{{if .HasFlags}}
func (v *{{.Name}}) computeFlags() uint32 {
	var flags uint32
{{- range .FlagFields}}
	if {{.Condition}} {
		flags |= 1 << {{.Bit}}
	}
{{- end}}
	return flags
}
{{end}}

{{.SerializeTL}}

{{.DeserializeTL}}
`

// GenerateType emits all constructor structs for a type declaration.
func (g *TypeGenerator) GenerateType(decl *parser.TypeDecl) error {
	if len(decl.Constructors) == 1 && !g.schema.IsLayered {
		// Single constructor type - generate struct with type name
		return g.GenerateSingleConstructorType(decl)
	}
	// Union type - generate constructor structs
	for i := range decl.Constructors {
		if g.isBaseType(&decl.Constructors[i]) {
			// Skip generating base MTProto types - they're in tlrpc/types
			continue
		}
		if err := g.GenerateConstructor(&decl.Constructors[i]); err != nil {
			return err
		}
	}
	return g.generateFamilyDecoder(decl)
}

// GenerateSingleConstructorType generates a struct with the type name for single-constructor types
func (g *TypeGenerator) GenerateSingleConstructorType(decl *parser.TypeDecl) error {
	if len(decl.Constructors) != 1 {
		return fmt.Errorf("expected single constructor, got %d", len(decl.Constructors))
	}

	ctor := &decl.Constructors[0]
	if g.isBaseType(ctor) {
		return nil // Skip base types
	}

	name := typeName(g.namer, decl.Name, decl.VariantLayer)
	if _, exists := g.emittedTypes[name]; exists {
		return nil
	}
	g.emittedTypes[name] = struct{}{}

	// Build field data
	var fields []FieldTemplateData
	for _, param := range ctor.Params {
		if shouldSkipParam(param) {
			continue
		}
		fieldName := g.namer.FieldName(param.Name)
		fieldType := g.goType(param.Type)
		fields = append(fields, FieldTemplateData{
			Name: fieldName,
			Type: fieldType,
		})
	}

	// Build flag field data
	flagsParam := findFlagsParam(ctor)
	var flagFields []FlagFieldTemplateData
	if flagsParam != nil {
		for _, param := range ctor.Params {
			bit := flagBit(param)
			if bit == nil {
				continue
			}
			if flagSetName(param) != "flags" {
				continue
			}
			fieldName := g.namer.FieldName(param.Name)
			flagFields = append(flagFields, FlagFieldTemplateData{
				Condition: flagCondition(param, fieldName),
				Bit:       *bit,
			})
		}
	}

	// Generate serialization methods as strings
	var serializeTL, deserializeTL string
	if err := g.generateSerializeTLString(ctor, name, &serializeTL); err != nil {
		return err
	}
	if err := g.generateDeserializeTLString(ctor, name, &deserializeTL); err != nil {
		return err
	}

	data := ConstructorTemplateData{
		Name:          name,
		Fields:        fields,
		ID:            ctor.ID,
		TLName:        ctor.Name,
		HasFlags:      flagsParam != nil,
		FlagFields:    flagFields,
		SerializeTL:   serializeTL,
		DeserializeTL: deserializeTL,
		IsLayered:     g.schema.IsLayered,
	}

	tmpl, err := template.New("single_constructor").Funcs(templateFuncMap()).Parse(constructorTemplate)
	if err != nil {
		return err
	}

	return tmpl.Execute(g.out, data)
}

// GenerateInterface emits a polymorphic interface for union types.
func (g *TypeGenerator) GenerateInterface(decl *parser.TypeDecl) error {
	if (!decl.IsUnion || len(decl.Constructors) == 1) && !g.schema.IsLayered {
		return nil
	}
	baseName := typeName(g.namer, decl.Name, 0)
	name := baseName + "Type"
	if _, exists := g.emittedTypes[name]; exists {
		return nil
	}
	g.emittedTypes[name] = struct{}{}

	var constructors []string
	seenConstructors := make(map[string]struct{})
	for i := range g.schema.Types {
		family := &g.schema.Types[i]
		if family.Name != decl.Name {
			continue
		}
		for j := range family.Constructors {
			if g.isBaseType(&family.Constructors[j]) {
				continue
			}
			ctorName := concreteTypeName(g.namer, g.schema, family.Constructors[j])
			if _, exists := seenConstructors[ctorName]; exists {
				continue
			}
			seenConstructors[ctorName] = struct{}{}
			constructors = append(constructors, ctorName)
		}
	}
	if len(constructors) == 0 {
		return nil
	}

	data := InterfaceTemplateData{
		Name:         name,
		BaseName:     baseName,
		Constructors: constructors,
	}

	tmpl, err := template.New("interface").Funcs(templateFuncMap()).Parse(interfaceTemplate)
	if err != nil {
		return err
	}

	return tmpl.Execute(g.out, data)
}

// isBaseType checks if a constructor represents a base MTProto type
func (g *TypeGenerator) isBaseType(ctor *parser.Constructor) bool {
	// Only truly primitive types that don't participate in unions
	baseTypeNames := map[string]bool{
		"boolFalse": true, // bool false constructor
		"boolTrue":  true, // bool true constructor
		"true":      true, // unit type
		"false":     true, // unit type (if it exists)
		"error":     true, // error type
		"null":      true, // null type
		"string":    true, // primitive string
		"bytes":     true, // primitive bytes
		"int128":    true, // primitive int
		"int256":    true, // primitive int
		"double":    true, // primitive float
		"vector":    true, // generic vector constructor
	}

	return baseTypeNames[ctor.Name]
}

// GenerateConstructor emits a single constructor struct and its methods.
func (g *TypeGenerator) GenerateConstructor(ctor *parser.Constructor) error {
	if len(ctor.GenericParams) > 0 || ctor.ResultType.IsTypeVar {
		return nil
	}

	name := concreteTypeName(g.namer, g.schema, *ctor)
	if _, exists := g.emittedTypes[name]; exists {
		return nil
	}
	g.emittedTypes[name] = struct{}{}

	// Build field data
	var fields []FieldTemplateData
	for _, param := range ctor.Params {
		if shouldSkipParam(param) {
			continue
		}
		fieldName := g.namer.FieldName(param.Name)
		fieldType := g.goType(param.Type)
		fields = append(fields, FieldTemplateData{
			Name: fieldName,
			Type: fieldType,
		})
	}

	// Build flag field data
	flagsParam := findFlagsParam(ctor)
	var flagFields []FlagFieldTemplateData
	if flagsParam != nil {
		for _, param := range ctor.Params {
			bit := flagBit(param)
			if bit == nil {
				continue
			}
			if flagSetName(param) != "flags" {
				continue
			}
			fieldName := g.namer.FieldName(param.Name)
			flagFields = append(flagFields, FlagFieldTemplateData{
				Condition: flagCondition(param, fieldName),
				Bit:       *bit,
			})
		}
	}

	// Generate serialization methods as strings
	var serializeTL, deserializeTL string
	if err := g.generateSerializeTLString(ctor, name, &serializeTL); err != nil {
		return err
	}
	if err := g.generateDeserializeTLString(ctor, name, &deserializeTL); err != nil {
		return err
	}

	data := ConstructorTemplateData{
		Name:          name,
		Fields:        fields,
		ID:            ctor.ID,
		TLName:        ctor.Name,
		HasFlags:      flagsParam != nil,
		FlagFields:    flagFields,
		SerializeTL:   serializeTL,
		DeserializeTL: deserializeTL,
		IsLayered:     g.schema.IsLayered,
	}

	tmpl, err := template.New("constructor").Funcs(templateFuncMap()).Parse(constructorTemplate)
	if err != nil {
		return err
	}

	return tmpl.Execute(g.out, data)
}

// generateSerializeTLString generates SerializeTL method as a string
func (g *TypeGenerator) generateSerializeTLString(ctor *parser.Constructor, name string, result *string) error {
	var buf bytes.Buffer
	oldOut := g.out
	g.out = &buf
	defer func() { g.out = oldOut }()

	if err := g.generateSerializeTL(ctor, name); err != nil {
		return err
	}

	*result = buf.String()
	return nil
}

// generateDeserializeTLString generates DeserializeTL method as a string
func (g *TypeGenerator) generateDeserializeTLString(ctor *parser.Constructor, name string, result *string) error {
	var buf bytes.Buffer
	oldOut := g.out
	g.out = &buf
	defer func() { g.out = oldOut }()

	if err := g.generateDeserializeTL(ctor, name); err != nil {
		return err
	}

	*result = buf.String()
	return nil
}

func findFlagsParam(ctor *parser.Constructor) *parser.Parameter {
	for i := range ctor.Params {
		param := &ctor.Params[i]
		if param.Name == "flags" && isFlagParam(*param) {
			return param
		}
	}
	return nil
}

func flagBit(param parser.Parameter) *int {
	if param.FlagBit != nil {
		return param.FlagBit
	}
	return param.Type.FlagBit
}

func isFlagParam(param parser.Parameter) bool {
	return strings.HasPrefix(param.Name, "flags") && param.Type.Name == "#"
}

func flagParamName(param parser.Parameter) string {
	if isFlagParam(param) {
		return param.Name
	}
	return "flags"
}

func flagSetName(param parser.Parameter) string {
	if param.Type.FlagName != "" {
		return param.Type.FlagName
	}
	if param.FlagBit != nil || param.Type.FlagBit != nil {
		return "flags"
	}
	return ""
}

func listFlagSets(params []parser.Parameter) []string {
	out := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	for _, param := range params {
		if !isFlagParam(param) {
			continue
		}
		name := flagParamName(param)
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func flagSetUsage(params []parser.Parameter) map[string]bool {
	usage := make(map[string]bool)
	for _, param := range params {
		if isFlagParam(param) && !shouldSkipParam(param) {
			usage[flagParamName(param)] = true
		}
		if flagBit(param) != nil {
			usage[flagSetName(param)] = true
		}
	}
	return usage
}

func flagCondition(param parser.Parameter, fieldName string) string {
	if isTrueType(param.Type) {
		return "v." + fieldName
	}
	return "v." + fieldName + " != nil"
}

func shouldSkipParam(param parser.Parameter) bool {
	if isFlagParam(param) && param.Name == "flags" {
		return true
	}
	if strings.EqualFold(param.Type.Name, "true") && param.Type.FlagBit != nil {
		return false
	}
	return false
}

func (g *TypeGenerator) goType(t parser.TypeRef) string {
	base := g.goBaseType(t)
	if t.Optional && !isTrueType(t) && !isUnionType(g.schema, t) && !strings.HasPrefix(base, "[]") {
		return "*" + base
	}
	return base
}

func (g *TypeGenerator) goBaseType(t parser.TypeRef) string {
	if t.IsVector && t.Generic != nil {
		elementBase := g.goBaseTypeNonVector(*t.Generic)
		if shouldUsePointerForType(g.schema, *t.Generic) {
			return "[]*" + elementBase
		}
		return "[]" + elementBase
	}
	return g.goBaseTypeNonVector(t)
}

func (g *TypeGenerator) goBaseTypeNonVector(t parser.TypeRef) string {
	if t.IsVector && t.Generic != nil {
		elementBase := g.goBaseTypeNonVector(*t.Generic)
		if shouldUsePointerForType(g.schema, *t.Generic) {
			return "[]*" + elementBase
		}
		return "[]" + elementBase
	}

	if isUnionType(g.schema, t) {
		return unionInterfaceName(g.namer, t)
	}
	if name, ok := bareConcreteTypeName(g.namer, g.schema, t); ok {
		return name
	}

	if t.Namespace != "" {
		return typeName(g.namer, t.Namespace+"."+t.Name, t.VariantLayer)
	}

	// Check for base MTProto types that are in the types package
	switch t.Name {
	case "true", "false":
		return "bool"
	case "error", "null":
		g.usesBaseTypes = true
		if t.Name == "error" {
			return "Error"
		}
		return "Null"
	case "string":
		return "string"
	case "bytes":
		return "[]byte"
	case "int128":
		g.usesBaseTypes = true
		return "Int128"
	case "int256":
		g.usesBaseTypes = true
		return "Int256"
	case "double":
		g.usesBaseTypes = true
		return "Double"
	case "vector":
		g.usesBaseTypes = true
		if t.Generic != nil {
			return "Vector[" + g.goBaseType(*t.Generic) + "]"
		}
		return "Vector[interface{}]"
	}

	switch t.Name {
	case "int":
		return "int32"
	case "long":
		return "int64"
	case "bytes":
		return "[]byte"
	case "Bool", "bool", "true", "false":
		return "bool"
	case "#":
		return "uint32"
	default:
		return typeName(g.namer, t.Name, t.VariantLayer)
	}
}

func isTrueType(t parser.TypeRef) bool {
	return strings.EqualFold(t.Name, "true")
}

// GenerateSchemaMetadata exposes generation-only schema metadata to consumers.
// Runtime dispatch remains constructor-driven and does not select API layers.
func GenerateSchemaMetadata(out io.Writer, layer int) error {
	_, err := fmt.Fprintf(out, "// SchemaLayer is the layer represented by this generated package.\nconst SchemaLayer = %d\n\n", layer)
	return err
}

// GenerateLayeredSchemaMetadata exposes the supported layer set while keeping
// SchemaLayer as the maximum layer for Runtime v2 wrapper clamping.
func GenerateLayeredSchemaMetadata(out io.Writer, baseLayer int, layers []int) error {
	if _, err := fmt.Fprintf(out, "// SchemaBaseLayer is the zero-layer compatibility default.\nconst SchemaBaseLayer = %d\n\n", baseLayer); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out, "// SchemaLayers lists every generated layer in ascending order.\nvar SchemaLayers = []int{%s}\n\n", joinInts(layers))
	return err
}

func joinInts(values []int) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = fmt.Sprintf("%d", value)
	}
	return strings.Join(parts, ", ")
}

// GenerateConstructorConstants writes constructor IDs as constants.
func GenerateConstructorConstants(namer *naming.Namer, out io.Writer, ctors []parser.Constructor) error {
	buf := &bytes.Buffer{}
	for i := range ctors {
		name := constructorName(namer, ctors[i])
		if _, err := fmt.Fprintf(buf, "const %sConstructorID uint32 = 0x%08x\n", name, ctors[i].ID); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(buf, "\n"); err != nil {
		return err
	}
	_, err := io.Copy(out, buf)
	return err
}
