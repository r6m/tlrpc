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

func TestServiceGenerator_MiniSchemaGolden(t *testing.T) {
	data := readTestSchema(t, "mini.tl")
	parser := parser.NewParser(string(data))
	schema, err := parser.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var servicesBuf bytes.Buffer
	gen := NewServiceGenerator(naming.NewNamer(), schema, &servicesBuf)
	if err := gen.GenerateService(schema.Functions); err != nil {
		t.Fatalf("generate service: %v", err)
	}

	goldenPath := filepath.Join("testdata", "mini_service.golden")
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	normalize := func(s string) string {
		return strings.Join(strings.Fields(s), " ")
	}

	got := servicesBuf.String()
	if normalize(got) != normalize(string(want)) {
		t.Fatalf("service output mismatch:\n--- want ---\n%s\n--- got ---\n%s", string(want), got)
	}
	if bytes.Contains([]byte(got), []byte("*InputPeerType")) {
		t.Fatalf("expected union return types to avoid pointer-to-interface")
	}
}
