// Package testutil provides testing utilities.
package testutil

import (
	"context"
	"testing"

	"github.com/r6m/tlrpc"
)

// TestServer creates a test server.
func TestServer(t testing.TB, opts ...tlrpc.ServerOption) *tlrpc.Server {
	server := tlrpc.NewServer(opts...)
	return server
}

// TestClient creates a test client.
func TestClient(t testing.TB, server *tlrpc.Server) *tlrpc.Client {
	// TODO: Implement test client
	return nil
}

// TestContext creates a test context.
func TestContext() context.Context {
	return context.Background()
}

// AssertNoError asserts that there is no error.
func AssertNoError(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// AssertError asserts that there is an error.
func AssertError(t testing.TB, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error but got none")
	}
}

// AssertEqual asserts that two values are equal.
func AssertEqual(t testing.TB, expected, actual interface{}) {
	t.Helper()
	if expected != actual {
		t.Fatalf("expected %v, got %v", expected, actual)
	}
}
