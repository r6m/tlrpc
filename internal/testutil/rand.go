// Package testutil provides testing utilities.
package testutil

import "crypto/rand"

// RandBytes returns n cryptographically random bytes.
func RandBytes(n int) []byte {
	buf := make([]byte, n)
	_, err := rand.Read(buf)
	Must(err)
	return buf
}
