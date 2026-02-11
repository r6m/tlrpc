package testutil_test

import (
	"testing"

	"github.com/r6m/tlrpc/internal/testutil"
)

func TestRandBytes(t *testing.T) {
	// Test different sizes
	for _, size := range []int{0, 1, 16, 1024} {
		b := testutil.RandBytes(t, size)
		if len(b) != size {
			t.Errorf("RandBytes(%d) length = %d; want %d", size, len(b), size)
		}
	}
}

func TestRandInt64(t *testing.T) {
	// Test with max = 0
	if n := testutil.RandInt64(t, 0); n != 0 {
		t.Errorf("RandInt64(0) = %d; want 0", n)
	}

	// Test with max = 1
	for i := 0; i < 100; i++ {
		n := testutil.RandInt64(t, 1)
		if n < 0 || n > 1 {
			t.Errorf("RandInt64(1) = %d; want 0 or 1", n)
		}
	}

	// Test with max = 100
	for i := 0; i < 100; i++ {
		n := testutil.RandInt64(t, 100)
		if n < 0 || n > 100 {
			t.Errorf("RandInt64(100) = %d; want 0-100", n)
		}
	}
}

func TestRandInt(t *testing.T) {
	// Test with max = 0
	if n := testutil.RandInt(t, 0); n != 0 {
		t.Errorf("RandInt(0) = %d; want 0", n)
	}

	// Test with max = 10
	for i := 0; i < 100; i++ {
		n := testutil.RandInt(t, 10)
		if n < 0 || n > 10 {
			t.Errorf("RandInt(10) = %d; want 0-10", n)
		}
	}
}

func TestRandBool(t *testing.T) {
	// Test that we get both true and false values (with high probability)
	hasTrue := false
	hasFalse := false

	for i := 0; i < 1000 && (!hasTrue || !hasFalse); i++ {
		b := testutil.RandBool(t)
		if b {
			hasTrue = true
		} else {
			hasFalse = true
		}
	}

	if !hasTrue {
		t.Error("RandBool should generate true values")
	}
	if !hasFalse {
		t.Error("RandBool should generate false values")
	}
}

func TestRandString(t *testing.T) {
	// Test different lengths
	for _, length := range []int{0, 1, 10, 100} {
		s := testutil.RandString(t, length)
		if len(s) != length {
			t.Errorf("RandString(%d) length = %d; want %d", length, len(s), length)
		}

		// Check that string contains only valid characters
		for _, r := range s {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
				t.Errorf("RandString generated invalid character: %c", r)
			}
		}
	}
}