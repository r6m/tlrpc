package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Must panics if err is not nil. Useful for tests where we want to fail fast.
func Must(err error) {
	if err != nil {
		panic(fmt.Sprintf("testutil.Must: %v", err))
	}
}

// Must2 is like Must but for functions that return (T, error).
func Must2[T any](v T, err error) T {
	Must(err)
	return v
}

// TempFile creates a temporary file for testing. The file will be cleaned up
// automatically when the test completes. Returns the file handle.
func TempFile(t testing.TB) *os.File {
	t.Helper()
	f, err := os.CreateTemp("", "tlrpc-test-*")
	Must(err)
	t.Cleanup(func() {
		f.Close()
		os.Remove(f.Name())
	})
	return f
}

// TempDir creates a temporary directory for testing. The directory and its
// contents will be cleaned up automatically when the test completes.
func TempDir(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tlrpc-test-*")
	Must(err)
	t.Cleanup(func() {
		os.RemoveAll(dir)
	})
	return dir
}

// WriteFile writes data to a file in the temp directory, creating parent
// directories as needed. The file will be cleaned up with the temp directory.
func WriteFile(t testing.TB, dir, filename string, data []byte) {
	t.Helper()
	path := filepath.Join(dir, filename)
	Must(os.MkdirAll(filepath.Dir(path), 0755))
	Must(os.WriteFile(path, data, 0644))
}

// ReadFile reads a file from the filesystem.
func ReadFile(t testing.TB, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	Must(err)
	return data
}
