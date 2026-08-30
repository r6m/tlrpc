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
