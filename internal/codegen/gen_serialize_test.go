package codegen

import (
	"bytes"
	"testing"
)

func TestTypeGenerator_SerializeMethods(t *testing.T) {
	data := readTestSchema(t, "simple.tl")
	parser := NewParser(string(data))
	schema, err := parser.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var typesBuf bytes.Buffer
	gen := NewTypeGenerator(NewNamer(), &typesBuf)
	for i := range schema.Types {
		if err := gen.GenerateType(&schema.Types[i]); err != nil {
			t.Fatalf("generate type: %v", err)
		}
	}

	content := typesBuf.String()
	if !contains(content, "SerializeTL") {
		t.Fatalf("expected SerializeTL method in output")
	}
	if !contains(content, "DeserializeTL") {
		t.Fatalf("expected DeserializeTL method in output")
	}
	if !contains(content, "computeFlags") {
		t.Fatalf("expected computeFlags helper in output")
	}
}
