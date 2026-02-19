package mtproto

import "testing"

func TestSeqNoGenerator(t *testing.T) {
	gen := NewSeqNoGenerator(0)
	if got := gen.Next(false); got != 0 {
		t.Fatalf("expected seq 0 for non-content, got %d", got)
	}
	if got := gen.Next(true); got != 3 {
		t.Fatalf("expected seq 3 for content, got %d", got)
	}
	if got := gen.Next(false); got != 2 {
		t.Fatalf("expected seq 2 for non-content, got %d", got)
	}
	if got := gen.Value(); got != 1 {
		t.Fatalf("expected value 1, got %d", got)
	}
}
