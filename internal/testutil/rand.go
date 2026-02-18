package testutil

import (
	"crypto/rand"
	"math/big"
	"testing"
)

// RandBytes generates n cryptographically secure random bytes.
func RandBytes(t testing.TB, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	_, err := rand.Read(b)
	Must(err)
	return b
}

// RandInt64 generates a random int64 between 0 and maxVal (inclusive).
func RandInt64(t testing.TB, maxVal int64) int64 {
	t.Helper()
	if maxVal <= 0 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(maxVal+1))
	Must(err)
	return n.Int64()
}

// RandInt generates a random int between 0 and maxVal (inclusive).
func RandInt(t testing.TB, maxVal int) int {
	t.Helper()
	return int(RandInt64(t, int64(maxVal)))
}

// RandBool generates a random boolean value.
func RandBool(t testing.TB) bool {
	t.Helper()
	return RandInt(t, 1) == 1
}

// RandString generates a random string of length n using printable ASCII characters.
func RandString(t testing.TB, n int) string {
	t.Helper()
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[RandInt(t, len(chars)-1)]
	}
	return string(b)
}
