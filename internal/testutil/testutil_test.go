package testutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/r6m/tlrpc/internal/testutil"
)

func TestMust(t *testing.T) {
	// Must should not panic with nil error
	testutil.Must(nil)

	// Must should panic with non-nil error
	defer func() {
		if r := recover(); r == nil {
			t.Error("Must should panic with non-nil error")
		}
	}()
	testutil.Must(os.ErrNotExist)
}

func TestMust2(t *testing.T) {
	// Must2 should return value when error is nil
	result := testutil.Must2(42, nil)
	if result != 42 {
		t.Errorf("Must2(42, nil) = %d; want 42", result)
	}

	// Must2 should panic when error is not nil
	defer func() {
		if r := recover(); r == nil {
			t.Error("Must2 should panic with non-nil error")
		}
	}()
	testutil.Must2(0, os.ErrNotExist)
}

func TestTempFile(t *testing.T) {
	f := testutil.TempFile(t)
	defer f.Close()

	// File should exist
	if _, err := os.Stat(f.Name()); os.IsNotExist(err) {
		t.Error("TempFile should create an existing file")
	}

	// Write some data
	data := []byte("test data")
	if _, err := f.Write(data); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}

	// File should be cleaned up after test (verified by t.Cleanup)
}

func TestTempDir(t *testing.T) {
	dir := testutil.TempDir(t)

	// Directory should exist
	if info, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("TempDir should create an existing directory")
	} else if !info.IsDir() {
		t.Error("TempDir should create a directory")
	}

	// Directory should be cleaned up after test (verified by t.Cleanup)
}

func TestWriteFile(t *testing.T) {
	dir := testutil.TempDir(t)

	// Write a file
	data := []byte("test content")
	testutil.WriteFile(t, dir, "subdir/file.txt", data)

	// File should exist with correct content
	path := filepath.Join(dir, "subdir/file.txt")
	content := testutil.ReadFile(t, path)
	if string(content) != string(data) {
		t.Errorf("WriteFile content = %q; want %q", content, data)
	}
}

func TestReadFile(t *testing.T) {
	dir := testutil.TempDir(t)
	path := filepath.Join(dir, "test.txt")
	expected := []byte("test data")

	if err := os.WriteFile(path, expected, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	content := testutil.ReadFile(t, path)
	if string(content) != string(expected) {
		t.Errorf("ReadFile content = %q; want %q", content, expected)
	}
}
