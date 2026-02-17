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
