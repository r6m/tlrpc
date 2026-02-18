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
}

// NewTypeGenerator creates a new type generator.
func NewTypeGenerator(namer *naming.Namer, out io.Writer, schema *parser.Schema) *TypeGenerator {
	return &TypeGenerator{namer: namer, out: out, schema: schema}
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
	if len(decl.Constructors) == 1 {
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
	return nil
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

	name := g.namer.TypeName(decl.Name)

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
			fieldName := g.namer.FieldName(param.Name)
			flagFields = append(flagFields, FlagFieldTemplateData{
				Condition: flagCondition(param, fieldName),
				Bit:       *bit,
			})
		}
	}

	// Generate serialization methods as strings
	var serializeTL, deserializeTL string
	if err := g.generateSerializeTLString(ctor, name, flagsParam != nil, &serializeTL); err != nil {
		return err
	}
	if err := g.generateDeserializeTLString(ctor, name, flagsParam != nil, &deserializeTL); err != nil {
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
	}

	tmpl, err := template.New("single_constructor").Funcs(templateFuncMap()).Parse(constructorTemplate)
	if err != nil {
		return err
	}

	return tmpl.Execute(g.out, data)
}

// GenerateInterface emits a polymorphic interface for union types.
func (g *TypeGenerator) GenerateInterface(decl *parser.TypeDecl) error {
	if !decl.IsUnion || len(decl.Constructors) == 1 {
		return nil
	}
	baseName := g.namer.TypeName(decl.Name)
	name := baseName + "Type"

	var constructors []string
	for i := range decl.Constructors {
		if g.isBaseType(&decl.Constructors[i]) {
			continue
		}
		ctorName := g.namer.ConstructorName(decl.Constructors[i].Name)
		constructors = append(constructors, ctorName)
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

	name := g.namer.ConstructorName(ctor.Name)

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
			fieldName := g.namer.FieldName(param.Name)
			flagFields = append(flagFields, FlagFieldTemplateData{
				Condition: flagCondition(param, fieldName),
				Bit:       *bit,
			})
		}
	}

	// Generate serialization methods as strings
	var serializeTL, deserializeTL string
	if err := g.generateSerializeTLString(ctor, name, flagsParam != nil, &serializeTL); err != nil {
		return err
	}
	if err := g.generateDeserializeTLString(ctor, name, flagsParam != nil, &deserializeTL); err != nil {
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
	}

	tmpl, err := template.New("constructor").Funcs(templateFuncMap()).Parse(constructorTemplate)
	if err != nil {
		return err
	}

	return tmpl.Execute(g.out, data)
}

// generateSerializeTLString generates SerializeTL method as a string
func (g *TypeGenerator) generateSerializeTLString(ctor *parser.Constructor, name string, hasFlags bool, result *string) error {
	var buf bytes.Buffer
	oldOut := g.out
	g.out = &buf
	defer func() { g.out = oldOut }()

	if err := g.generateSerializeTL(ctor, name, hasFlags); err != nil {
		return err
	}

	*result = buf.String()
	return nil
}

// generateDeserializeTLString generates DeserializeTL method as a string
func (g *TypeGenerator) generateDeserializeTLString(ctor *parser.Constructor, name string, hasFlags bool, result *string) error {
	var buf bytes.Buffer
	oldOut := g.out
	g.out = &buf
	defer func() { g.out = oldOut }()

	if err := g.generateDeserializeTL(ctor, name, hasFlags); err != nil {
		return err
	}

	*result = buf.String()
	return nil
}

func (g *TypeGenerator) generateSerializeMethods(ctor *parser.Constructor, name string) error {
	flagsParam := findFlagsParam(ctor)
	if flagsParam != nil {
		if err := g.generateFlagsHelper(ctor, name); err != nil {
			return err
		}
	}
	if err := g.generateSerializeTL(ctor, name, flagsParam != nil); err != nil {
		return err
	}
	if err := g.generateDeserializeTL(ctor, name, flagsParam != nil); err != nil {
		return err
	}
	return nil
}

func (g *TypeGenerator) generateFlagsHelper(ctor *parser.Constructor, name string) error {
	if _, err := fmt.Fprintf(g.out, "func (v *%s) computeFlags() uint32 {\n\tvar flags uint32\n", name); err != nil {
		return err
	}
	for _, param := range ctor.Params {
		bit := flagBit(param)
		if bit == nil {
			continue
		}
		fieldName := g.namer.FieldName(param.Name)
		if isTrueType(param.Type) {
			if _, err := fmt.Fprintf(g.out, "\tif v.%s {\n\t\tflags |= 1 << %d\n\t}\n", fieldName, *bit); err != nil {
				return err
			}
			continue
		}
		if param.Type.Optional {
			if _, err := fmt.Fprintf(g.out, "\tif v.%s != nil {\n\t\tflags |= 1 << %d\n\t}\n", fieldName, *bit); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(g.out, "\tif v.%s != nil {\n\t\tflags |= 1 << %d\n\t}\n", fieldName, *bit); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(g.out, "\treturn flags\n}\n\n"); err != nil {
		return err
	}
	return nil
}

func (g *TypeGenerator) generateSerializeTL(ctor *parser.Constructor, name string, hasFlags bool) error {
	if _, err := fmt.Fprintf(g.out, "func (v *%s) SerializeTL(w io.Writer) error {\n", name); err != nil {
		return err
	}
	if !ctor.IsBare {
		if _, err := fmt.Fprintf(g.out, "\tif err := mtproto.WriteUint32(w, v.ConstructorID()); err != nil {\n\t\treturn err\n\t}\n"); err != nil {
			return err
		}
	}
	if hasFlags {
		if _, err := io.WriteString(g.out, "\tflags := v.computeFlags()\n\tif err := mtproto.WriteUint32(w, flags); err != nil {\n\t\treturn err\n\t}\n"); err != nil {
			return err
		}
	}
	for _, param := range ctor.Params {
		if shouldSkipParam(param) {
			continue
		}
		bit := flagBit(param)
		fieldName := g.namer.FieldName(param.Name)
		if isTrueType(param.Type) {
			continue
		}
		if bit != nil {
			if _, err := fmt.Fprintf(g.out, "\tif flags&(1<<%d) != 0 {\n", *bit); err != nil {
				return err
			}
			if err := g.writeSerializeField(param, fieldName, "\t\t"); err != nil {
				return err
			}
			if _, err := io.WriteString(g.out, "\t}\n"); err != nil {
				return err
			}
			continue
		}
		if err := g.writeSerializeField(param, fieldName, "\t"); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(g.out, "\treturn nil\n}\n\n"); err != nil {
		return err
	}
	return nil
}

func (g *TypeGenerator) writeSerializeField(param parser.Parameter, fieldName, indent string) error {
	typeRef := param.Type
	if typeRef.IsVector && typeRef.Generic != nil {
		if _, err := fmt.Fprintf(g.out, "%sif err := mtproto.WriteVectorHeader(w, len(v.%s)); err != nil {\n%s\treturn err\n%s}\n", indent, fieldName, indent, indent); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%sfor i := range v.%s {\n", indent, fieldName); err != nil {
			return err
		}
		if err := g.writeSerializeValue(*typeRef.Generic, fmt.Sprintf("v.%s[i]", fieldName), indent+"\t"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s}\n", indent); err != nil {
			return err
		}
		return nil
	}
	return g.writeSerializeValue(typeRef, "v."+fieldName, indent)
}

func (g *TypeGenerator) writeSerializeValue(t parser.TypeRef, value, indent string) error {
	if t.Optional && !isTrueType(t) {
		base := parser.TypeRef{Name: t.Name, Namespace: t.Namespace, IsVector: t.IsVector, Generic: t.Generic}
		if needsOptionalPointer(t, g.goBaseType(base), g.schema) {
			if _, ok := serializeBuiltinCall(base, value); ok {
				return g.writeSerializeValue(base, "*"+value, indent)
			}
			return g.writeSerializeValue(base, value, indent)
		}
	}
	writeCall, ok := serializeBuiltinCall(t, value)
	if ok {
		_, err := fmt.Fprintf(g.out, "%s%s\n", indent, writeCall)
		return err
	}
	_, err := fmt.Fprintf(g.out, "%sif err := %s.SerializeTL(w); err != nil {\n%s\treturn err\n%s}\n", indent, value, indent, indent)
	return err
}

func (g *TypeGenerator) generateDeserializeTL(ctor *parser.Constructor, name string, hasFlags bool) error {
	if _, err := fmt.Fprintf(g.out, "func (v *%s) DeserializeTL(r io.Reader) error {\n", name); err != nil {
		return err
	}
	if !ctor.IsBare {
		if _, err := io.WriteString(g.out, "\tctorID, err := mtproto.ReadUint32(r)\n\tif err != nil {\n\t\treturn err\n\t}\n"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "\tif ctorID != v.ConstructorID() {\n\t\treturn fmt.Errorf(\"wrong constructor: got %%x, want %%x\", ctorID, v.ConstructorID())\n\t}\n"); err != nil {
			return err
		}
	}
	usesFlags := hasFlags && hasFlaggedParams(ctor.Params)
	if hasFlags {
		if usesFlags {
			if _, err := io.WriteString(g.out, "\tflags, err := mtproto.ReadUint32(r)\n\tif err != nil {\n\t\treturn err\n\t}\n"); err != nil {
				return err
			}
		} else {
			if _, err := io.WriteString(g.out, "\t_, err = mtproto.ReadUint32(r)\n\tif err != nil {\n\t\treturn err\n\t}\n"); err != nil {
				return err
			}
		}
	}
	for _, param := range ctor.Params {
		if shouldSkipParam(param) {
			continue
		}
		bit := flagBit(param)
		fieldName := g.namer.FieldName(param.Name)
		if isTrueType(param.Type) {
			if bit != nil {
				if _, err := fmt.Fprintf(g.out, "\tv.%s = flags&(1<<%d) != 0\n", fieldName, *bit); err != nil {
					return err
				}
			}
			continue
		}
		if bit != nil {
			if _, err := fmt.Fprintf(g.out, "\tif flags&(1<<%d) != 0 {\n", *bit); err != nil {
				return err
			}
			if err := g.writeDeserializeField(param, fieldName, "\t\t"); err != nil {
				return err
			}
			if _, err := io.WriteString(g.out, "\t}\n"); err != nil {
				return err
			}
			continue
		}
		if err := g.writeDeserializeField(param, fieldName, "\t"); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(g.out, "\treturn nil\n}\n\n"); err != nil {
		return err
	}
	return nil
}

func (g *TypeGenerator) writeDeserializeField(param parser.Parameter, fieldName, indent string) error {
	typeRef := param.Type
	if typeRef.IsVector && typeRef.Generic != nil {
		elementType := g.goBaseType(*typeRef.Generic)
		if _, err := fmt.Fprintf(g.out, "%s{\n", indent); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s\tvar items []%s\n", indent, elementType); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s\tif err := mtproto.ReadVector(r, func() error {\n", indent); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s\t\tvar item %s\n", indent, elementType); err != nil {
			return err
		}
		if err := g.writeDeserializeValue(*typeRef.Generic, "item", indent+"\t\t"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s\t\titems = append(items, item)\n%s\t\treturn nil\n%s\t}); err != nil {\n%s\t\treturn err\n%s\t}\n", indent, indent, indent, indent, indent); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s\tv.%s = items\n", indent, fieldName); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s}\n", indent); err != nil {
			return err
		}
		return nil
	}
	return g.writeDeserializeValue(typeRef, "v."+fieldName, indent)
}

func (g *TypeGenerator) writeDeserializeValue(t parser.TypeRef, target, indent string) error {
	if t.Optional && !isTrueType(t) && needsOptionalPointer(t, g.goBaseType(t), g.schema) {
		base := parser.TypeRef{Name: t.Name, Namespace: t.Namespace, IsVector: t.IsVector, Generic: t.Generic}
		if readCall, ok := deserializeBuiltinCall(base); ok {
			if _, err := fmt.Fprintf(g.out, "%s{\n%s\tvalue, err := %s\n%s\tif err != nil {\n%s\t\treturn err\n%s\t}\n", indent, indent, readCall, indent, indent, indent); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(g.out, "%s\tval := value\n%s\t%s = &val\n%s}\n", indent, indent, target, indent); err != nil {
				return err
			}
			return nil
		}
		baseName := g.goBaseType(base)
		if _, err := fmt.Fprintf(g.out, "%svar value %s\n", indent, baseName); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%sif err := value.DeserializeTL(r); err != nil {\n%s\treturn err\n%s}\n", indent, indent, indent); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s%s = &value\n", indent, target); err != nil {
			return err
		}
		return nil
	}
	readCall, ok := deserializeBuiltinCall(t)
	if ok {
		if _, err := fmt.Fprintf(g.out, "%s{\n%s\tvalue, err := %s\n%s\tif err != nil {\n%s\t\treturn err\n%s\t}\n", indent, indent, readCall, indent, indent, indent); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s\t%s = value\n%s}\n", indent, target, indent); err != nil {
			return err
		}
		return nil
	}
	if _, err := fmt.Fprintf(g.out, "%sif err := %s.DeserializeTL(r); err != nil {\n%s\treturn err\n%s}\n", indent, target, indent, indent); err != nil {
		return err
	}
	return nil
}

func findFlagsParam(ctor *parser.Constructor) *parser.Parameter {
	for i := range ctor.Params {
		param := &ctor.Params[i]
		if param.Name == "flags" && param.Type.Name == "#" {
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

func hasFlaggedParams(params []parser.Parameter) bool {
	for _, param := range params {
		if flagBit(param) != nil {
			return true
		}
	}
	return false
}

func flagCondition(param parser.Parameter, fieldName string) string {
	if isTrueType(param.Type) {
		return "v." + fieldName
	}
	return "v." + fieldName + " != nil"
}

func serializeBuiltinCall(t parser.TypeRef, value string) (string, bool) {
	switch t.Name {
	case "int":
		return fmt.Sprintf("if err := mtproto.WriteInt32(w, %s); err != nil {\n\treturn err\n}", value), true
	case "long":
		return fmt.Sprintf("if err := mtproto.WriteInt64(w, %s); err != nil {\n\treturn err\n}", value), true
	case "int128":
		// types.Int128 is [16]byte, so convert it
		return fmt.Sprintf("if err := mtproto.WriteInt128(w, [16]byte(%s)); err != nil {\n\treturn err\n}", value), true
	case "int256":
		// types.Int256 is [32]byte, so convert it
		return fmt.Sprintf("if err := mtproto.WriteInt256(w, [32]byte(%s)); err != nil {\n\treturn err\n}", value), true
	case "double":
		// types.Double is float64, so convert it
		return fmt.Sprintf("if err := mtproto.WriteDouble(w, float64(%s)); err != nil {\n\treturn err\n}", value), true
	case "string":
		return fmt.Sprintf("if err := mtproto.WriteString(w, %s); err != nil {\n\treturn err\n}", value), true
	case "bytes":
		return fmt.Sprintf("if err := mtproto.WriteBytes(w, %s); err != nil {\n\treturn err\n}", value), true
	case "Bool", "bool", "true", "false":
		return fmt.Sprintf("if err := mtproto.WriteBool(w, %s); err != nil {\n\treturn err\n}", value), true
	case "#":
		return fmt.Sprintf("if err := mtproto.WriteUint32(w, %s); err != nil {\n\treturn err\n}", value), true
	default:
		return "", false
	}
}

func deserializeBuiltinCall(t parser.TypeRef) (string, bool) {
	return deserializeBuiltinCallWithReader(t, "r")
}

func deserializeBuiltinCallWithReader(t parser.TypeRef, readerName string) (string, bool) {
	switch t.Name {
	case "int":
		return fmt.Sprintf("mtproto.ReadInt32(%s)", readerName), true
	case "long":
		return fmt.Sprintf("mtproto.ReadInt64(%s)", readerName), true
	case "int128":
		return fmt.Sprintf("func() (Int128, error) { v, err := mtproto.ReadInt128(%s); return Int128(v), err }()", readerName), true
	case "int256":
		return fmt.Sprintf("func() (Int256, error) { v, err := mtproto.ReadInt256(%s); return Int256(v), err }()", readerName), true
	case "double":
		return fmt.Sprintf("func() (Double, error) { v, err := mtproto.ReadDouble(%s); return Double(v), err }()", readerName), true
	case "string":
		return fmt.Sprintf("mtproto.ReadString(%s)", readerName), true
	case "bytes":
		return fmt.Sprintf("mtproto.ReadBytes(%s)", readerName), true
	case "Bool", "bool", "true", "false":
		return fmt.Sprintf("mtproto.ReadBool(%s)", readerName), true
	case "#":
		return fmt.Sprintf("mtproto.ReadUint32(%s)", readerName), true
	default:
		return "", false
	}
}

func shouldSkipParam(param parser.Parameter) bool {
	if param.Name == "flags" && param.Type.Name == "#" {
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
		return "[]" + g.goBaseType(*t.Generic)
	}

	if isUnionType(g.schema, t) {
		return unionInterfaceName(g.namer, t)
	}

	if t.Namespace != "" {
		return g.namer.TypeName(t.Namespace + "." + t.Name)
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
		return g.namer.TypeName(t.Name)
	}
}

func isTrueType(t parser.TypeRef) bool {
	return strings.EqualFold(t.Name, "true")
}

// GenerateConstructorConstants writes constructor IDs as constants.
func GenerateConstructorConstants(namer *naming.Namer, out io.Writer, ctors []parser.Constructor) error {
	buf := &bytes.Buffer{}
	for i := range ctors {
		name := namer.ConstructorName(ctors[i].Name)
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
