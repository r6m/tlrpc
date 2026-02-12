package codegen

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

// TypeGenerator generates Go types from TL declarations.
type TypeGenerator struct {
	namer          *Namer
	out            io.Writer
	importsWritten bool
}

// NewTypeGenerator creates a new type generator.
func NewTypeGenerator(namer *Namer, out io.Writer) *TypeGenerator {
	return &TypeGenerator{namer: namer, out: out}
}

// GenerateType emits all constructor structs for a type declaration.
func (g *TypeGenerator) GenerateType(decl *TypeDecl) error {
	for i := range decl.Constructors {
		if err := g.GenerateConstructor(&decl.Constructors[i]); err != nil {
			return err
		}
	}
	return nil
}

// GenerateInterface emits a polymorphic interface for union types.
func (g *TypeGenerator) GenerateInterface(decl *TypeDecl) error {
	if !decl.IsUnion {
		return nil
	}
	baseName := g.namer.TypeName(decl.Name)
	name := baseName + "Type"
	_, err := fmt.Fprintf(g.out, "type %s interface {\n\tis%sType()\n\tConstructorID() uint32\n\tTLName() string\n}\n\n", name, baseName)
	if err != nil {
		return err
	}
	for i := range decl.Constructors {
		ctorName := g.namer.ConstructorName(decl.Constructors[i].Name)
		if _, err := fmt.Fprintf(g.out, "func (*%s) is%sType() {}\n", ctorName, baseName); err != nil {
			return err
		}
	}
	_, err = io.WriteString(g.out, "\n")
	return err
}

// GenerateConstructor emits a single constructor struct and its methods.
func (g *TypeGenerator) GenerateConstructor(ctor *Constructor) error {
	if len(ctor.GenericParams) > 0 || ctor.ResultType.IsTypeVar {
		return nil
	}
	if err := g.writeImports(); err != nil {
		return err
	}

	name := g.namer.ConstructorName(ctor.Name)
	if _, err := fmt.Fprintf(g.out, "type %s struct {\n", name); err != nil {
		return err
	}

	for _, param := range ctor.Params {
		if shouldSkipParam(param) {
			continue
		}
		fieldName := g.namer.FieldName(param.Name)
		fieldType := g.goType(param.Type)
		if _, err := fmt.Fprintf(g.out, "\t%s %s\n", fieldName, fieldType); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(g.out, "}\n\n"); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(g.out, "func (v *%s) ConstructorID() uint32 { return 0x%08x }\n", name, ctor.ID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "func (v *%s) Method() string { return \"\" }\n", name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "func (v *%s) TLName() string { return %q }\n\n", name, ctor.Name); err != nil {
		return err
	}
	if err := g.generateSerializeMethods(ctor, name); err != nil {
		return err
	}

	return nil
}

func (g *TypeGenerator) writeImports() error {
	if g.importsWritten {
		return nil
	}
	g.importsWritten = true
	_, err := io.WriteString(g.out, "import (\n\t\"fmt\"\n\t\"io\"\n\t\"github.com/r6m/tlrpc/mtproto\"\n)\n\n")
	return err
}

func (g *TypeGenerator) generateSerializeMethods(ctor *Constructor, name string) error {
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

func (g *TypeGenerator) generateFlagsHelper(ctor *Constructor, name string) error {
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

func (g *TypeGenerator) generateSerializeTL(ctor *Constructor, name string, hasFlags bool) error {
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

func (g *TypeGenerator) writeSerializeField(param Parameter, fieldName, indent string) error {
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

func (g *TypeGenerator) writeSerializeValue(t TypeRef, value, indent string) error {
	if t.Optional && !isTrueType(t) {
		base := TypeRef{Name: t.Name, Namespace: t.Namespace, IsVector: t.IsVector, Generic: t.Generic}
		return g.writeSerializeValue(base, "*"+value, indent)
	}
	writeCall, ok := serializeBuiltinCall(t, value)
	if ok {
		_, err := fmt.Fprintf(g.out, "%s%s\n", indent, writeCall)
		return err
	}
	_, err := fmt.Fprintf(g.out, "%sif err := %s.SerializeTL(w); err != nil {\n%s\treturn err\n%s}\n", indent, value, indent, indent)
	return err
}

func (g *TypeGenerator) generateDeserializeTL(ctor *Constructor, name string, hasFlags bool) error {
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
	if hasFlags {
		if _, err := io.WriteString(g.out, "\tflags, err := mtproto.ReadUint32(r)\n\tif err != nil {\n\t\treturn err\n\t}\n"); err != nil {
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

func (g *TypeGenerator) writeDeserializeField(param Parameter, fieldName, indent string) error {
	typeRef := param.Type
	if typeRef.IsVector && typeRef.Generic != nil {
		elementType := g.goBaseType(*typeRef.Generic)
		if _, err := fmt.Fprintf(g.out, "%svar items []%s\n", indent, elementType); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%sif err := mtproto.ReadVector(r, func() error {\n", indent); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s\tvar item %s\n", indent, elementType); err != nil {
			return err
		}
		if err := g.writeDeserializeValue(*typeRef.Generic, "item", indent+"\t"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s\titems = append(items, item)\n%s\treturn nil\n%s}); err != nil {\n%s\treturn err\n%s}\n", indent, indent, indent, indent, indent); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%sv.%s = items\n", indent, fieldName); err != nil {
			return err
		}
		return nil
	}
	return g.writeDeserializeValue(typeRef, "v."+fieldName, indent)
}

func (g *TypeGenerator) writeDeserializeValue(t TypeRef, target, indent string) error {
	if t.Optional && !isTrueType(t) {
		base := TypeRef{Name: t.Name, Namespace: t.Namespace, IsVector: t.IsVector, Generic: t.Generic}
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

func findFlagsParam(ctor *Constructor) *Parameter {
	for i := range ctor.Params {
		param := &ctor.Params[i]
		if param.Name == "flags" && param.Type.Name == "#" {
			return param
		}
	}
	return nil
}

func flagBit(param Parameter) *int {
	if param.FlagBit != nil {
		return param.FlagBit
	}
	return param.Type.FlagBit
}

func serializeBuiltinCall(t TypeRef, value string) (string, bool) {
	switch t.Name {
	case "int":
		return fmt.Sprintf("if err := mtproto.WriteInt32(w, %s); err != nil {\n\treturn err\n}", value), true
	case "long":
		return fmt.Sprintf("if err := mtproto.WriteInt64(w, %s); err != nil {\n\treturn err\n}", value), true
	case "int128":
		return fmt.Sprintf("if err := mtproto.WriteInt128(w, %s); err != nil {\n\treturn err\n}", value), true
	case "int256":
		return fmt.Sprintf("if err := mtproto.WriteInt256(w, %s); err != nil {\n\treturn err\n}", value), true
	case "double":
		return fmt.Sprintf("if err := mtproto.WriteDouble(w, %s); err != nil {\n\treturn err\n}", value), true
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

func deserializeBuiltinCall(t TypeRef) (string, bool) {
	switch t.Name {
	case "int":
		return "mtproto.ReadInt32(r)", true
	case "long":
		return "mtproto.ReadInt64(r)", true
	case "int128":
		return "mtproto.ReadInt128(r)", true
	case "int256":
		return "mtproto.ReadInt256(r)", true
	case "double":
		return "mtproto.ReadDouble(r)", true
	case "string":
		return "mtproto.ReadString(r)", true
	case "bytes":
		return "mtproto.ReadBytes(r)", true
	case "Bool", "bool", "true", "false":
		return "mtproto.ReadBool(r)", true
	case "#":
		return "mtproto.ReadUint32(r)", true
	default:
		return "", false
	}
}

func shouldSkipParam(param Parameter) bool {
	if param.Name == "flags" && param.Type.Name == "#" {
		return true
	}
	if strings.EqualFold(param.Type.Name, "true") && param.Type.FlagBit != nil {
		return false
	}
	return false
}

func (g *TypeGenerator) goType(t TypeRef) string {
	base := g.goBaseType(t)
	if t.Optional && !isTrueType(t) {
		return "*" + base
	}
	return base
}

func (g *TypeGenerator) goBaseType(t TypeRef) string {
	if t.IsVector && t.Generic != nil {
		return "[]" + g.goBaseType(*t.Generic)
	}

	if t.Namespace != "" {
		return g.namer.TypeName(t.Namespace + "." + t.Name)
	}

	switch t.Name {
	case "int":
		return "int32"
	case "long":
		return "int64"
	case "int128":
		return "[16]byte"
	case "int256":
		return "[32]byte"
	case "double":
		return "float64"
	case "string":
		return "string"
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

func isTrueType(t TypeRef) bool {
	return strings.EqualFold(t.Name, "true")
}

// GenerateConstructorConstants writes constructor IDs as constants.
func GenerateConstructorConstants(namer *Namer, out io.Writer, ctors []Constructor) error {
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
