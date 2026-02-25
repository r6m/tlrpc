package generator

import (
	"bytes"
	"strings"
	"testing"

	"github.com/r6m/tlrpc/internal/naming"
	"github.com/r6m/tlrpc/internal/parser"
)

func TestTypeGenerator_SerializeMethods(t *testing.T) {
	data := readTestSchema(t, "simple.tl")
	parser := parser.NewParser(string(data))
	schema, err := parser.Parse()
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
	if !strings.Contains(content, "SerializeTL") {
		t.Fatalf("expected SerializeTL method in output")
	}
	if !strings.Contains(content, "DeserializeTL") {
		t.Fatalf("expected DeserializeTL method in output")
	}
	if !strings.Contains(content, "computeFlags") {
		t.Fatalf("expected computeFlags helper in output")
	}
}

func TestTypeGenerator_MultiFlagSets(t *testing.T) {
	const schemaText = `---types---
multiFlags#01020304 flags:# flags2:# a:flags.0?true b:flags2.1?true c:flags2.2?string = MultiFlags;
`
	p := parser.NewParser(schemaText)
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
	if !strings.Contains(content, "flags2 := uint32(0)") {
		t.Fatalf("expected flags2 local variable in serialize path")
	}
	if !strings.Contains(content, "if v.B {\n\t\tflags2 |= 1 << 1") {
		t.Fatalf("expected flags2 bool bit computation")
	}
	if !strings.Contains(content, "if flags2&(1<<2) != 0") {
		t.Fatalf("expected flags2 bit gate for optional fields")
	}
	if !strings.Contains(content, "if err := mtproto.WriteUint32(w, flags2); err != nil") {
		t.Fatalf("expected flags2 field serialization")
	}
	if !strings.Contains(content, "v.B = flags2&(1<<1) != 0") {
		t.Fatalf("expected flags2 bool assignment in deserialize")
	}
}
