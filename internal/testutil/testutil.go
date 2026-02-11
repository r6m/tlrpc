// Package testutil provides testing utilities.
package testutil

import (
	"os"
)

// Must panics if err is non-nil.
func Must(err error) {
	if err != nil {
		panic(err)
	}
}

// TempFile creates a temporary file for tests.
func TempFile() *os.File {
	file, err := os.CreateTemp("", "tlrpc-test-*")
	Must(err)
	return file
}
