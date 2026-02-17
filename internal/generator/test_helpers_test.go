package generator

import (
	"os"
	"path/filepath"
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
