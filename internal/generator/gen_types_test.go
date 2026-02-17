package generator

import (
	"bytes"
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
