package generator

import (
	"fmt"
	"io"

	"github.com/r6m/tlrpc/internal/naming"
	"github.com/r6m/tlrpc/internal/parser"
)

// cursorCodec emits the shared field body used by boxed, bare, request, and
// typed-response codecs. Generated code stays on concrete mtproto cursors.
type cursorCodec struct {
	out      io.Writer
	schema   *parser.Schema
	namer    *naming.Namer
	receiver string
	cursor   string
	goBase   func(parser.TypeRef) string
}

func (c cursorCodec) writeSerializeParams(params []parser.Parameter, indent string) error {
	for _, param := range params {
		bit := flagBit(param)
		if isFlagParam(param) {
			if _, err := fmt.Fprintf(c.out, "%sif err := %s.WriteUint32(%s); err != nil {\n%s\treturn err\n%s}\n", indent, c.cursor, flagParamName(param), indent, indent); err != nil {
				return err
			}
			continue
		}
		if shouldSkipParam(param) || isTrueType(param.Type) {
			continue
		}
		field := c.receiver + "." + c.namer.FieldName(param.Name)
		if bit != nil {
			if _, err := fmt.Fprintf(c.out, "%sif %s&(1<<%d) != 0 {\n", indent, flagSetName(param), *bit); err != nil {
				return err
			}
			if err := c.writeSerializeValue(param.Type, field, indent+"\t"); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(c.out, "%s}\n", indent); err != nil {
				return err
			}
			continue
		}
		if err := c.writeSerializeValue(param.Type, field, indent); err != nil {
			return err
		}
	}
	return nil
}

func (c cursorCodec) writeSerializeValue(t parser.TypeRef, value, indent string) error {
	if t.Optional && !isTrueType(t) {
		base := t
		base.Optional, base.FlagBit = false, nil
		if needsOptionalPointer(t, c.goBase(base), c.schema) {
			if _, ok := cursorWriteCall(c.cursor, base, value); ok {
				return c.writeSerializeValue(base, "*"+value, indent)
			}
			return c.writeSerializeValue(base, value, indent)
		}
	}
	if t.IsVector && t.Generic != nil {
		if _, err := fmt.Fprintf(c.out, "%sif err := func() error {\n%s\tif err := %s.EnterObject(); err != nil { return err }\n%s\tdefer %s.LeaveObject()\n%s\tif err := %s.WriteVectorHeader(len(%s)); err != nil { return err }\n%s\tfor _, element := range %s {\n", indent, indent, c.cursor, indent, c.cursor, indent, c.cursor, value, indent, value); err != nil {
			return err
		}
		if err := c.writeSerializeValue(*t.Generic, "element", indent+"\t"); err != nil {
			return err
		}
		_, err := fmt.Fprintf(c.out, "%s\t}\n%s\treturn nil\n%s}(); err != nil { return err }\n", indent, indent, indent)
		return err
	}
	if call, ok := cursorWriteCall(c.cursor, t, value); ok {
		_, err := fmt.Fprintf(c.out, "%s%s\n", indent, call)
		return err
	}
	if t.IsBare {
		_, err := fmt.Fprintf(c.out, "%sif err := %s.serializeTLBare(%s); err != nil {\n%s\treturn err\n%s}\n", indent, value, c.cursor, indent, indent)
		return err
	}
	if isUnionType(c.schema, t) {
		if _, err := fmt.Fprintf(c.out, "%sif %s == nil {\n%s\treturn fmt.Errorf(\"required boxed %s is nil\")\n%s}\n", indent, value, indent, t.FullName(), indent); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(c.out, "%sif err := %s.SerializeTL(%s); err != nil {\n%s\treturn err\n%s}\n", indent, value, c.cursor, indent, indent)
	return err
}

func (c cursorCodec) writeDeserializeParams(ctor *parser.Constructor, params []parser.Parameter, layer, indent string) error {
	usage := flagSetUsage(params)
	for _, set := range listFlagSets(params) {
		if usage[set] {
			if _, err := fmt.Fprintf(c.out, "%svar %s uint32\n", indent, set); err != nil {
				return err
			}
		}
	}
	for _, param := range params {
		bit := flagBit(param)
		if isFlagParam(param) {
			set := flagParamName(param)
			if !usage[set] && !c.schema.IsLayered && shouldSkipParam(param) {
				if _, err := fmt.Fprintf(c.out, "%sif _, err := %s.ReadUint32(); err != nil { return err }\n", indent, c.cursor); err != nil {
					return err
				}
				continue
			}
			if _, err := fmt.Fprintf(c.out, "%s{\n%s\tvalue, err := %s.ReadUint32()\n%s\tif err != nil { return err }\n", indent, indent, c.cursor, indent); err != nil {
				return err
			}
			if usage[set] {
				if _, err := fmt.Fprintf(c.out, "%s\t%s = value\n", indent, set); err != nil {
					return err
				}
			}
			if c.schema.IsLayered {
				if err := writeKnownFlagValidation(c.out, params, set, "value", layer, indent+"\t"); err != nil {
					return err
				}
			}
			if !shouldSkipParam(param) {
				if _, err := fmt.Fprintf(c.out, "%s\t%s.%s = value\n", indent, c.receiver, c.namer.FieldName(param.Name)); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(c.out, "%s}\n", indent); err != nil {
				return err
			}
			continue
		}
		if shouldSkipParam(param) {
			continue
		}
		field := c.receiver + "." + c.namer.FieldName(param.Name)
		if isTrueType(param.Type) {
			if bit != nil {
				if _, err := fmt.Fprintf(c.out, "%s%s = %s&(1<<%d) != 0\n", indent, field, flagSetName(param), *bit); err != nil {
					return err
				}
			}
			continue
		}
		if bit != nil {
			if _, err := fmt.Fprintf(c.out, "%sif %s&(1<<%d) != 0 {\n", indent, flagSetName(param), *bit); err != nil {
				return err
			}
			if err := c.writeDeserializeValue(param.Type, field, indent+"\t"); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(c.out, "%s}\n", indent); err != nil {
				return err
			}
			continue
		}
		if err := c.writeDeserializeValue(param.Type, field, indent); err != nil {
			return err
		}
	}
	return nil
}

func (c cursorCodec) writeDeserializeValue(t parser.TypeRef, target, indent string) error {
	return c.writeDeserializeValueDepth(t, target, indent, 0)
}

func (c cursorCodec) writeDeserializeValueDepth(t parser.TypeRef, target, indent string, depth int) error {
	if t.IsVector && t.Generic != nil {
		elem := *t.Generic
		elemType := c.goBase(elem)
		elemPointer := !isUnionType(c.schema, elem) && shouldUsePointerForType(c.schema, elem)
		if elemPointer {
			elemType = "*" + elemType
		}
		minBytes := minimumElementBytes(elem)
		index := fmt.Sprintf("index%d", depth)
		if _, err := fmt.Fprintf(c.out, "%sif err := func() error {\n%s\tif err := %s.EnterObject(); err != nil { return err }\n%s\tdefer %s.LeaveObject()\n%s\tcount, err := %s.ReadVectorCount(mtproto.MaxVectorElements, %d)\n%s\tif err != nil { return err }\n", indent, indent, c.cursor, indent, c.cursor, indent, c.cursor, minBytes, indent); err != nil {
			return err
		}
		if fixedElementBytes(elem) > 0 {
			if _, err := fmt.Fprintf(c.out, "%s\titems := make([]%s, count)\n%s\tfor %s := 0; %s < count; %s++ {\n", indent, elemType, indent, index, index, index); err != nil {
				return err
			}
			if err := c.writeDeserializeValueDepth(elem, "items["+index+"]", indent+"\t\t", depth+1); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintf(c.out, "%s\tcapacity := count\n%s\tif capacity > 1024 { capacity = 1024 }\n%s\titems := make([]%s, 0, capacity)\n%s\tfor %s := 0; %s < count; %s++ {\n", indent, indent, indent, elemType, indent, index, index, index); err != nil {
				return err
			}
			if elemPointer && !isBoxedSingletonPointer(c.schema, elem) {
				if _, err := fmt.Fprintf(c.out, "%s\t\titem := &%s{}\n", indent, c.goBase(elem)); err != nil {
					return err
				}
			} else if _, err := fmt.Fprintf(c.out, "%s\t\tvar item %s\n", indent, elemType); err != nil {
				return err
			}
			if err := c.writeDeserializeValueDepth(elem, "item", indent+"\t\t", depth+1); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(c.out, "%s\t\titems = append(items, item)\n", indent); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintf(c.out, "%s\t}\n%s\t%s = items\n%s\treturn nil\n%s}(); err != nil { return err }\n", indent, indent, target, indent, indent)
		return err
	}
	if isUnionType(c.schema, t) {
		helper := "decode" + unionInterfaceName(c.namer, t)
		_, err := fmt.Fprintf(c.out, "%s{\n%s\tvalue, err := %s(%s)\n%s\tif err != nil { return err }\n%s\t%s = value\n%s}\n", indent, indent, helper, c.cursor, indent, indent, target, indent)
		return err
	}
	if isBoxedSingletonPointer(c.schema, t) {
		_, err := fmt.Fprintf(c.out, "%s%s = &%s{}\n%sif err := %s.deserializeTL(%s); err != nil { return err }\n", indent, target, c.goBase(t), indent, target, c.cursor)
		return err
	}
	if t.Optional && !isTrueType(t) && needsOptionalPointer(t, c.goBase(t), c.schema) {
		base := t
		base.Optional, base.FlagBit = false, nil
		if call, ok := cursorReadCall(c.cursor, base); ok {
			_, err := fmt.Fprintf(c.out, "%s{\n%s\tdecoded, err := %s\n%s\tif err != nil { return err }\n%s\t%s = &decoded\n%s}\n", indent, indent, call, indent, indent, target, indent)
			return err
		}
		baseType := c.goBase(base)
		if _, err := fmt.Fprintf(c.out, "%s{\n%s\tvar value %s\n", indent, indent, baseType); err != nil {
			return err
		}
		if err := c.writeDeserializeValueDepth(base, "value", indent+"\t", depth); err != nil {
			return err
		}
		_, err := fmt.Fprintf(c.out, "%s\t%s = &value\n%s}\n", indent, target, indent)
		return err
	}
	if call, ok := cursorReadCall(c.cursor, t); ok {
		if _, err := fmt.Fprintf(c.out, "%s{\n%s\tvalue, err := %s\n%s\tif err != nil { return err }\n%s\t%s = value\n%s}\n", indent, indent, call, indent, indent, target, indent); err != nil {
			return err
		}
		return nil
	}
	method := "deserializeTL"
	if t.IsBare {
		method = "deserializeTLBare"
	}
	_, err := fmt.Fprintf(c.out, "%sif err := %s.%s(%s); err != nil {\n%s\treturn err\n%s}\n", indent, target, method, c.cursor, indent, indent)
	return err
}

func cursorWriteCall(cursor string, t parser.TypeRef, value string) (string, bool) {
	switch t.Name {
	case "int":
		return fmt.Sprintf("if err := %s.WriteInt32(%s); err != nil { return err }", cursor, value), true
	case "long":
		return fmt.Sprintf("if err := %s.WriteInt64(%s); err != nil { return err }", cursor, value), true
	case "int128":
		return fmt.Sprintf("if err := %s.WriteInt128([16]byte(%s)); err != nil { return err }", cursor, value), true
	case "int256":
		return fmt.Sprintf("if err := %s.WriteInt256([32]byte(%s)); err != nil { return err }", cursor, value), true
	case "double":
		return fmt.Sprintf("if err := %s.WriteDouble(float64(%s)); err != nil { return err }", cursor, value), true
	case "string":
		return fmt.Sprintf("if err := %s.WriteString(%s); err != nil { return err }", cursor, value), true
	case "bytes":
		return fmt.Sprintf("if err := %s.WriteBytes(%s); err != nil { return err }", cursor, value), true
	case "Bool", "bool", "true", "false":
		return fmt.Sprintf("if err := %s.WriteBool(%s); err != nil { return err }", cursor, value), true
	case "#":
		return fmt.Sprintf("if err := %s.WriteUint32(%s); err != nil { return err }", cursor, value), true
	default:
		return "", false
	}
}

func cursorReadCall(cursor string, t parser.TypeRef) (string, bool) {
	switch t.Name {
	case "int":
		return cursor + ".ReadInt32()", true
	case "long":
		return cursor + ".ReadInt64()", true
	case "int128":
		return fmt.Sprintf("func() (Int128, error) { v, err := %s.ReadInt128(); return Int128(v), err }()", cursor), true
	case "int256":
		return fmt.Sprintf("func() (Int256, error) { v, err := %s.ReadInt256(); return Int256(v), err }()", cursor), true
	case "double":
		return fmt.Sprintf("func() (Double, error) { v, err := %s.ReadDouble(); return Double(v), err }()", cursor), true
	case "string":
		return cursor + ".ReadString()", true
	case "bytes":
		return cursor + ".ReadBytes()", true
	case "Bool", "bool", "true", "false":
		return cursor + ".ReadBool()", true
	case "#":
		return cursor + ".ReadUint32()", true
	default:
		return "", false
	}
}

func fixedElementBytes(t parser.TypeRef) int {
	if t.Optional {
		return 0
	}
	switch t.Name {
	case "int", "#", "Bool", "bool", "true", "false":
		return 4
	case "long", "double":
		return 8
	case "int128":
		return 16
	case "int256":
		return 32
	default:
		return 0
	}
}

func minimumElementBytes(t parser.TypeRef) int {
	if fixed := fixedElementBytes(t); fixed > 0 {
		return fixed
	}
	if t.IsVector {
		return 8
	}
	if t.IsBare {
		return 0
	}
	switch t.Name {
	case "string", "bytes":
		return 4
	default:
		// Every boxed object starts with a constructor ID.
		return 4
	}
}

func (g *TypeGenerator) generateSerializeTL(ctor *parser.Constructor, name string) error {
	if _, err := fmt.Fprintf(g.out, "func (v *%s) SerializeTL(w io.Writer) error {\n\treturn v.serializeTL(mtproto.NewEncoder(w))\n}\n\n", name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "func (v *%s) serializeTL(e *mtproto.Encoder) error {\n\tif v == nil { return fmt.Errorf(\"serialize %s: nil receiver\") }\n", name, ctor.Name); err != nil {
		return err
	}
	if g.schema.IsLayered {
		if _, err := fmt.Fprintf(g.out, "\tlayer := e.TLLayer()\n\tif !%s { return fmt.Errorf(\"constructor %s is unavailable at layer %%d\", layer) }\n", layerSupportExpression("layer", ctor.Intervals), ctor.Name); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(g.out, "\tif err := e.EnterObject(); err != nil { return err }\n\tdefer e.LeaveObject()\n"); err != nil {
		return err
	}
	if !ctor.IsBare {
		if _, err := io.WriteString(g.out, "\tif err := e.WriteUint32(v.ConstructorID()); err != nil { return err }\n"); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(g.out, "\treturn v.serializeTLBody(e)\n}\n\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "func (v *%s) serializeTLBody(e *mtproto.Encoder) error {\n", name); err != nil {
		return err
	}
	if err := g.writeCursorSerializeSetup(ctor.Params, "v", "\t"); err != nil {
		return err
	}
	emitter := cursorCodec{out: g.out, schema: g.schema, namer: g.namer, receiver: "v", cursor: "e", goBase: g.goBaseType}
	if err := emitter.writeSerializeParams(ctor.Params, "\t"); err != nil {
		return err
	}
	if _, err := io.WriteString(g.out, "\treturn nil\n}\n\n"); err != nil {
		return err
	}
	if _, needed := g.bareTypes[name]; needed {
		return g.generateSerializeTLBare(ctor, name)
	}
	return nil
}

func (g *TypeGenerator) writeCursorSerializeSetup(params []parser.Parameter, receiver, indent string) error {
	for _, set := range listFlagSets(params) {
		if set == "flags" {
			if _, err := fmt.Fprintf(g.out, "%sflags := %s.computeFlags()\n", indent, receiver); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(g.out, "%s%s := uint32(0)\n", indent, set); err != nil {
			return err
		}
		for _, param := range params {
			bit := flagBit(param)
			if bit == nil || flagSetName(param) != set {
				continue
			}
			field := receiver + "." + g.namer.FieldName(param.Name)
			condition := field + " != nil"
			if isTrueType(param.Type) {
				condition = field
			}
			if _, err := fmt.Fprintf(g.out, "%sif %s { %s |= 1 << %d }\n", indent, condition, set, *bit); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *TypeGenerator) generateSerializeTLBare(ctor *parser.Constructor, name string) error {
	_, err := fmt.Fprintf(g.out, "func (v *%s) serializeTLBare(e *mtproto.Encoder) error {\n\tif v == nil { return fmt.Errorf(\"serialize bare %s: nil receiver\") }\n", name, ctor.Name)
	if err != nil {
		return err
	}
	if g.schema.IsLayered {
		if _, err = fmt.Fprintf(g.out, "\tlayer := e.TLLayer()\n\tif !%s { return fmt.Errorf(\"constructor %s is unavailable at layer %%d\", layer) }\n", layerSupportExpression("layer", ctor.Intervals), ctor.Name); err != nil {
			return err
		}
	}
	_, err = io.WriteString(g.out, "\tif err := e.EnterObject(); err != nil { return err }\n\tdefer e.LeaveObject()\n\treturn v.serializeTLBody(e)\n}\n\n")
	return err
}

func (g *TypeGenerator) generateDeserializeTL(ctor *parser.Constructor, name string) error {
	if _, err := fmt.Fprintf(g.out, "func (v *%s) DeserializeTL(r io.Reader) error {\n\treturn v.deserializeTL(mtproto.NewDecoder(r))\n}\n\n", name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "func (v *%s) deserializeTL(d *mtproto.Decoder) error {\n\tif v == nil { return fmt.Errorf(\"deserialize %s: nil receiver\") }\n", name, ctor.Name); err != nil {
		return err
	}
	if !ctor.IsBare {
		if _, err := io.WriteString(g.out, "\tconstructorID, err := d.ReadUint32()\n\tif err != nil { return err }\n\tif constructorID != v.ConstructorID() { return fmt.Errorf(\"wrong constructor: got %x, want %x\", constructorID, v.ConstructorID()) }\n"); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(g.out, "\treturn v.deserializeTLBody(d)\n}\n\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "func (v *%s) deserializeTLBody(d *mtproto.Decoder) error {\n\tif v == nil { return fmt.Errorf(\"deserialize %s body: nil receiver\") }\n\t*v = %s{}\n", name, ctor.Name, name); err != nil {
		return err
	}
	if g.schema.IsLayered {
		if _, err := fmt.Fprintf(g.out, "\tlayer := d.TLLayer()\n\tif !%s { return fmt.Errorf(\"constructor %s is unavailable at layer %%d\", layer) }\n", layerSupportExpression("layer", ctor.Intervals), ctor.Name); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(g.out, "\tif err := d.EnterObject(); err != nil { return err }\n\tdefer d.LeaveObject()\n"); err != nil {
		return err
	}
	emitter := cursorCodec{out: g.out, schema: g.schema, namer: g.namer, receiver: "v", cursor: "d", goBase: g.goBaseType}
	if err := emitter.writeDeserializeParams(ctor, ctor.Params, "layer", "\t"); err != nil {
		return err
	}
	if _, err := io.WriteString(g.out, "\treturn nil\n}\n\n"); err != nil {
		return err
	}
	if _, needed := g.bareTypes[name]; needed {
		return g.generateDeserializeTLBare(ctor, name)
	}
	return nil
}

func (g *TypeGenerator) generateDeserializeTLBare(ctor *parser.Constructor, name string) error {
	_, err := fmt.Fprintf(g.out, "func (v *%s) deserializeTLBare(d *mtproto.Decoder) error {\n\treturn v.deserializeTLBody(d)\n}\n\n", name)
	return err
}

func (g *TypeGenerator) generateFamilyDecoder(decl *parser.TypeDecl) error {
	if len(decl.Constructors) <= 1 {
		return nil
	}
	hasConcrete := false
	for _, family := range g.schema.Types {
		if family.Name != decl.Name {
			continue
		}
		for i := range family.Constructors {
			if !g.isBaseType(&family.Constructors[i]) {
				hasConcrete = true
				break
			}
		}
	}
	if !hasConcrete {
		return nil
	}
	iface := typeName(g.namer, decl.Name, 0) + "Type"
	key := "family-decoder:" + iface
	if _, ok := g.emittedTypes[key]; ok {
		return nil
	}
	g.emittedTypes[key] = struct{}{}
	if _, err := fmt.Fprintf(g.out, "func decode%s(d *mtproto.Decoder) (%s, error) {\n\tconstructorID, err := d.ReadUint32()\n\tif err != nil { return nil, err }\n", iface, iface); err != nil {
		return err
	}
	if g.schema.IsLayered {
		if _, err := io.WriteString(g.out, "\tobject, ok := NewConstructorForLayer(constructorID, d.TLLayer())\n"); err != nil {
			return err
		}
	} else {
		if _, err := io.WriteString(g.out, "\tnewObject, ok := GetStaticConstructors()[constructorID]\n\tvar object tlrpc.TLObject\n\tif ok { object = newObject() }\n"); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(g.out, "\tif !ok || object == nil { return nil, fmt.Errorf(\"unknown constructor: %x\", constructorID) }\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "\tvalue, ok := object.(%s)\n\tif !ok || value == nil { return nil, fmt.Errorf(\"constructor %%x is not %s\", constructorID) }\n\tswitch concrete := value.(type) {\n", iface, iface); err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, family := range g.schema.Types {
		if family.Name != decl.Name {
			continue
		}
		for _, ctor := range family.Constructors {
			if g.isBaseType(&ctor) {
				continue
			}
			name := concreteTypeName(g.namer, g.schema, ctor)
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			if _, err := fmt.Fprintf(g.out, "\tcase *%s:\n\t\tif concrete == nil { return nil, fmt.Errorf(\"constructor %%x produced nil %s\", constructorID) }\n\t\tif err := concrete.deserializeTLBody(d); err != nil { return nil, err }\n", name, name); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintf(g.out, "\tdefault:\n\t\treturn nil, fmt.Errorf(\"constructor %%x has unsupported %s implementation %%T\", constructorID, value)\n\t}\n\treturn value, nil\n}\n\n", iface)
	return err
}

func (g *ServiceGenerator) generateRequestSerialize(fn parser.FuncDecl, name string) error {
	if _, err := fmt.Fprintf(g.out, "func (r *%s) SerializeTL(w io.Writer) error {\n\treturn r.serializeTL(mtproto.NewEncoder(w))\n}\n\n", name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "func (r *%s) serializeTL(e *mtproto.Encoder) error {\n\tif r == nil { return fmt.Errorf(\"serialize %s: nil receiver\") }\n", name, fn.Name); err != nil {
		return err
	}
	if g.schema.IsLayered {
		if _, err := fmt.Fprintf(g.out, "\tlayer := e.TLLayer()\n\tif !%s { return fmt.Errorf(\"method %s is unavailable at layer %%d\", layer) }\n", layerSupportExpression("layer", fn.RequestIntervals), fn.Name); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(g.out, "\tif err := e.EnterObject(); err != nil { return err }\n\tdefer e.LeaveObject()\n\tif err := e.WriteUint32(r.ConstructorID()); err != nil { return err }\n\treturn r.serializeTLBody(e)\n}\n\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "func (r *%s) serializeTLBody(e *mtproto.Encoder) error {\n", name); err != nil {
		return err
	}
	if err := g.writeRequestCursorSerializeSetup(fn.Params, "\t"); err != nil {
		return err
	}
	emitter := cursorCodec{out: g.out, schema: g.schema, namer: g.namer, receiver: "r", cursor: "e", goBase: g.goBaseType}
	if err := emitter.writeSerializeParams(fn.Params, "\t"); err != nil {
		return err
	}
	_, err := io.WriteString(g.out, "\treturn nil\n}\n\n")
	return err
}

func (g *ServiceGenerator) writeRequestCursorSerializeSetup(params []parser.Parameter, indent string) error {
	for _, set := range listFlagSets(params) {
		if set == "flags" {
			if _, err := fmt.Fprintf(g.out, "%sflags := r.computeFlags()\n", indent); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(g.out, "%s%s := uint32(0)\n", indent, set); err != nil {
			return err
		}
		for _, param := range params {
			bit := flagBit(param)
			if bit == nil || flagSetName(param) != set {
				continue
			}
			field := "r." + g.namer.FieldName(param.Name)
			condition := field + " != nil"
			if isTrueType(param.Type) {
				condition = field
			}
			if _, err := fmt.Fprintf(g.out, "%sif %s { %s |= 1 << %d }\n", indent, condition, set, *bit); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *ServiceGenerator) generateRequestDeserialize(fn parser.FuncDecl, name string) error {
	if _, err := fmt.Fprintf(g.out, "func (r *%s) DeserializeTL(rd io.Reader) error {\n\treturn r.deserializeTL(mtproto.NewDecoder(rd))\n}\n\n", name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "func (r *%s) deserializeTL(d *mtproto.Decoder) error {\n\tif r == nil { return fmt.Errorf(\"deserialize %s: nil receiver\") }\n\tconstructorID, err := d.ReadUint32()\n\tif err != nil { return err }\n\tif constructorID != r.ConstructorID() { return fmt.Errorf(\"wrong constructor: got %%x, want %%x\", constructorID, r.ConstructorID()) }\n\treturn r.deserializeTLBody(d)\n}\n\n", name, fn.Name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "func (r *%s) deserializeTLBody(d *mtproto.Decoder) error {\n\tif r == nil { return fmt.Errorf(\"deserialize %s body: nil receiver\") }\n\t*r = %s{}\n", name, fn.Name, name); err != nil {
		return err
	}
	if g.schema.IsLayered {
		if _, err := fmt.Fprintf(g.out, "\tlayer := d.TLLayer()\n\tif !%s { return fmt.Errorf(\"method %s is unavailable at layer %%d\", layer) }\n", layerSupportExpression("layer", fn.RequestIntervals), fn.Name); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(g.out, "\tif err := d.EnterObject(); err != nil { return err }\n\tdefer d.LeaveObject()\n"); err != nil {
		return err
	}
	emitter := cursorCodec{out: g.out, schema: g.schema, namer: g.namer, receiver: "r", cursor: "d", goBase: g.goBaseType}
	pseudo := &parser.Constructor{Params: fn.Params}
	if err := emitter.writeDeserializeParams(pseudo, fn.Params, "layer", "\t"); err != nil {
		return err
	}
	_, err := io.WriteString(g.out, "\treturn nil\n}\n\n")
	return err
}
