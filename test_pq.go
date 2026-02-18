//go:build ignore
// +build ignore

package tlrpc

import (
	"fmt"
	"math/big"

	"github.com/r6m/tlrpc/crypto"
)

func main() {
	// Test the hardcoded PQ value
	pqBytes := []byte{0x17, 0xED, 0x48, 0x94, 0x1A, 0x08, 0xF9, 0x81}
	pq := new(big.Int).SetBytes(pqBytes)

	fmt.Printf("PQ bytes: %x\n", pqBytes)
	fmt.Printf("PQ value: %s\n", pq.String())

	factors, err := crypto.FactorizePQ(pq)
	if err != nil {
		fmt.Println("Error factorizing PQ:", err)
		return
	}

	fmt.Printf("P: %s, Q: %s\n", factors.P.String(), factors.Q.String())

	// Verify P * Q = PQ
	product := new(big.Int).Mul(factors.P, factors.Q)
	if product.Cmp(pq) == 0 {
		fmt.Println("✓ Factorization correct: P * Q = PQ")
	} else {
		fmt.Println("✗ Factorization incorrect")
	}
}
