package codegen

import (
	"bytes"
	"testing"
)

func TestCodecGenerator_SimpleSchema(t *testing.T) {
	data := readTestSchema(t, "simple.tl")
	parser := NewParser(string(data))
	schema, err := parser.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var buf bytes.Buffer
	gen := NewCodecGenerator(NewNamer(), &buf)
	if err := gen.Generate(schema); err != nil {
		t.Fatalf("generate codec: %v", err)
	}

	output := buf.String()
	if !contains(output, "func RegisterCodec") {
		t.Fatalf("expected RegisterCodec helper")
	}
	if !contains(output, "RegisterConstructor(0x8f97c628") {
		t.Fatalf("expected constructor registration for user")
	}
	if !contains(output, "RegisterMethod(\"auth.sendCode\"") {
		t.Fatalf("expected method registration for auth.sendCode")
	}
}
