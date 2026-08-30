package mtproto

import "testing"

func TestSeqNoGenerator(t *testing.T) {
	gen := NewSeqNoGenerator(0)
	if got := gen.Next(false); got != 0 {
		t.Fatalf("expected seq 0 for non-content, got %d", got)
	}
	if got := gen.Next(true); got != 1 {
		t.Fatalf("expected seq 1 for first content message, got %d", got)
	}
	if got := gen.Next(false); got != 2 {
		t.Fatalf("expected seq 2 for non-content, got %d", got)
	}
	if got := gen.Value(); got != 1 {
		t.Fatalf("expected value 1, got %d", got)
	}
}

func TestSeqNoGeneratorOddEvenRules(t *testing.T) {
	gen := NewSeqNoGenerator(0)

	first := gen.Next(true)
	if first%2 == 0 {
		t.Fatalf("expected content-related seq_no to be odd, got %d", first)
	}
	second := gen.Next(false)
	if second%2 != 0 {
		t.Fatalf("expected non-content seq_no to be even, got %d", second)
	}
	third := gen.Next(true)
	if third%2 == 0 {
		t.Fatalf("expected content-related seq_no to be odd, got %d", third)
	}
	if gen.Value() != 2 {
		t.Fatalf("expected content-related count 2, got %d", gen.Value())
	}
}
