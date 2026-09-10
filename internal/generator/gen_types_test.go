package generator

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r6m/tlrpc/internal/naming"
	"github.com/r6m/tlrpc/internal/parser"
)

func TestTypeGenerator_SimpleSchema(t *testing.T) {
	data := readTestSchema(t, "simple.tl")
	parser := parser.NewParser(string(data))
	schema, err := parser.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var typesBuf bytes.Buffer
	var ifaceBuf bytes.Buffer
	gen := NewTypeGenerator(naming.NewNamer(), &typesBuf, schema)
	ifaceGen := NewTypeGenerator(naming.NewNamer(), &ifaceBuf, schema)

	for i := range schema.Types {
		if err := gen.GenerateType(&schema.Types[i]); err != nil {
			t.Fatalf("generate type: %v", err)
		}
		if err := ifaceGen.GenerateInterface(&schema.Types[i]); err != nil {
			t.Fatalf("generate interface: %v", err)
		}
	}

	content := typesBuf.String()
	if !strings.Contains(content, "type User struct") {
		t.Fatalf("expected User struct in output")
	}
	if !strings.Contains(content, "FirstName *string") {
		t.Fatalf("expected FirstName pointer in output")
	}
	if !strings.Contains(content, "ConstructorID() uint32") {
		t.Fatalf("expected ConstructorID method in output")
	}

	iface := ifaceBuf.String()
	if !strings.Contains(iface, "type UserType interface") {
		t.Fatalf("expected UserType interface in output")
	}
	if !strings.Contains(iface, "func (*User) isUserType()") {
		t.Fatalf("expected User isUserType method in output")
	}
	if !strings.Contains(iface, "func (*UserEmpty) isUserType()") {
		t.Fatalf("expected UserEmpty isUserType method in output")
	}
}

func TestTypeGenerator_DoesNotGenerateMTProtoEnvelopeTypes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "schema-217.tl"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	p := parser.NewParser(string(data))
	schema, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var aliases bytes.Buffer
	if err := GenerateBaseAliases(&aliases); err != nil {
		t.Fatalf("generate aliases: %v", err)
	}

	var typesBuf bytes.Buffer
	var ifaceBuf bytes.Buffer
	gen := NewTypeGenerator(naming.NewNamer(), &typesBuf, schema)
	ifaceGen := NewTypeGenerator(naming.NewNamer(), &ifaceBuf, schema)
	for i := range schema.Types {
		if err := gen.GenerateType(&schema.Types[i]); err != nil {
			t.Fatalf("generate type: %v", err)
		}
		if err := ifaceGen.GenerateInterface(&schema.Types[i]); err != nil {
			t.Fatalf("generate interface: %v", err)
		}
	}
	all := aliases.String() + "\n" + typesBuf.String() + "\n" + ifaceBuf.String()

	if !strings.Contains(all, "type String = tltypes.String") {
		t.Fatalf("expected built-in alias import usage")
	}
	if strings.Contains(all, "type MsgContainer struct") {
		t.Fatalf("unexpected MTProto envelope type generated: MsgContainer")
	}
	if strings.Contains(all, "type MsgsAck struct") {
		t.Fatalf("unexpected MTProto envelope type generated: MsgsAck")
	}
	if strings.Contains(all, "type RPCResult struct") {
		t.Fatalf("unexpected MTProto envelope type generated: RPCResult")
	}
}

func TestTypeGenerator_UnionFieldDeserializeUsesConstructorDispatch(t *testing.T) {
	data := readTestSchema(t, "simple.tl")
	p := parser.NewParser(string(data))
	schema, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var typesBuf bytes.Buffer
	gen := NewTypeGenerator(naming.NewNamer(), &typesBuf, schema)
	for i := range schema.Types {
		if err := gen.GenerateType(&schema.Types[i]); err != nil {
			t.Fatalf("generate type: %v", err)
		}
	}
	content := typesBuf.String()
	if !strings.Contains(content, "decodeUserType(d)") {
		t.Fatalf("expected union constructor dispatch in DeserializeTL")
	}
	if !strings.Contains(content, "d.EnterObject()") {
		t.Fatalf("expected generated object decode budget entry")
	}
	if strings.Contains(content, "PrependReader") || strings.Contains(content, "bytes.Buffer") {
		t.Fatalf("cursor family decode must not replay constructor bytes")
	}
	if strings.Contains(content, "if err := v.User.DeserializeTL(r); err != nil {") {
		t.Fatalf("expected union field to avoid direct interface DeserializeTL call")
	}
}

func TestTypeGenerator_LayeredSingletonStaysConcreteAndKeepsConditionalBareCodec(t *testing.T) {
	base, err := parser.NewParser(`---types---
leaf#1 value:int = Leaf;
box#2 flags:# bare:!Leaf optional_bare:flags.0?!Leaf boxed:Leaf optional_boxed:flags.1?Leaf leaves:Vector<Leaf> = Box;`).ParseWithLayer(228)
	if err != nil {
		t.Fatal(err)
	}
	layered, err := parser.ResolveLayers(base, 228, nil)
	if err != nil {
		t.Fatal(err)
	}
	var types, interfaces bytes.Buffer
	typeGen := NewTypeGenerator(naming.NewNamer(), &types, layered.Schema)
	interfaceGen := NewTypeGenerator(naming.NewNamer(), &interfaces, layered.Schema)
	for i := range layered.Schema.Types {
		if err := typeGen.GenerateType(&layered.Schema.Types[i]); err != nil {
			t.Fatal(err)
		}
		if err := interfaceGen.GenerateInterface(&layered.Schema.Types[i]); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Contains(interfaces.String(), "type LeafType interface") || strings.Contains(interfaces.String(), "isLeafType") {
		t.Fatalf("layered singleton generated a speculative interface:\n%s", interfaces.String())
	}
	for _, field := range []string{"Bare Leaf", "OptionalBare *Leaf", "Boxed *Leaf", "OptionalBoxed *Leaf", "Leaves []*Leaf"} {
		if !strings.Contains(types.String(), field) {
			t.Fatalf("missing concrete singleton field %q:\n%s", field, types.String())
		}
	}
	if strings.Contains(types.String(), "decodeLeafType") {
		t.Fatalf("layered singleton generated a family decoder:\n%s", types.String())
	}
	if strings.Count(types.String(), "func (v *Leaf) serializeTLBare") != 1 || strings.Contains(types.String(), "func (v *Box) serializeTLBare") {
		t.Fatalf("bare helpers must be emitted only for referenced Leaf codec:\n%s", types.String())
	}
	start := strings.Index(types.String(), "func (v *Leaf) serializeTLBare")
	end := strings.Index(types.String()[start:], "func (v *Leaf) deserializeTLBare")
	if start < 0 || end < 0 || strings.Contains(types.String()[start:start+end], "WriteUint32(w, v.ConstructorID())") {
		t.Fatalf("bare serializer emitted a constructor tag")
	}
	if !strings.Contains(types.String(), "e.EnterObject()") {
		t.Fatalf("missing recursive encode guard")
	}
}
