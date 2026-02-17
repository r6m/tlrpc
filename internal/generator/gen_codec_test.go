package generator

import (
	"bytes"
	"strings"
	"testing"

	"github.com/r6m/tlrpc/internal/naming"
	"github.com/r6m/tlrpc/internal/parser"
)

func TestCodecGenerator_SimpleSchema(t *testing.T) {
	data := readTestSchema(t, "simple.tl")
	parser := parser.NewParser(string(data))
	schema, err := parser.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var buf bytes.Buffer
	gen := NewCodecGenerator(naming.NewNamer(), &buf)
	if err := gen.Generate(schema); err != nil {
		t.Fatalf("generate codec: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "var staticConstructors") {
		t.Fatalf("expected static constructor map")
	}
	if !strings.Contains(output, "0x8f97c628: func() tlrpc.TLObject") {
		t.Fatalf("expected constructor entry for user")
	}
	if !strings.Contains(output, "\"auth.sendCode\": func() tlrpc.TLObject") {
		t.Fatalf("expected static method constructor for auth.sendCode")
	}
	if !strings.Contains(output, "func GetStaticConstructors()") {
		t.Fatalf("expected GetStaticConstructors function")
	}
}
