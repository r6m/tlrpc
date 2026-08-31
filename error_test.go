package tlrpc

import (
	"errors"
	"fmt"
	"testing"
)

func TestWrappedRPCErrorPreservesStatus(t *testing.T) {
	want := NewBadRequestError("PHONE_CODE_INVALID")
	wrapped := fmt.Errorf("auth failed: %w", want)
	if got := FromError(wrapped); got != want {
		t.Fatalf("FromError = %#v, want wrapped RPC error", got)
	}
	got, ok := IsRPCError(wrapped)
	if !ok || got != want {
		t.Fatalf("IsRPCError = %#v, %t", got, ok)
	}
	if !errors.Is(wrapped, want) {
		t.Fatal("wrapped error lost errors.Is identity")
	}
}

func TestFromErrorRedactsUnknownError(t *testing.T) {
	const secret = "sentinel-database-password"

	got := FromError(errors.New(secret))
	if got.ErrorCode != int32(Internal) || got.ErrorMessage != "INTERNAL" {
		t.Fatalf("FromError = %#v, want 500 INTERNAL", got)
	}
	if got.ErrorMessage == secret {
		t.Fatal("unknown error text was exposed in RPC output")
	}
}
