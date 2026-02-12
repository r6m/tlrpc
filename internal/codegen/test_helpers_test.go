package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readTestSchema(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "schemas", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema %s: %v", name, err)
	}
	return data
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
