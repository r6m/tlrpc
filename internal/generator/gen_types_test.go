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
	if !strings.Contains(content, "GetStaticConstructors()[ctorID]") {
		t.Fatalf("expected union constructor dispatch in DeserializeTL")
	}
	if strings.Contains(content, "if err := v.User.DeserializeTL(r); err != nil {") {
		t.Fatalf("expected union field to avoid direct interface DeserializeTL call")
	}
}
