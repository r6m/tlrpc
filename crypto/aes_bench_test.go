package crypto

import (
	"crypto/aes"
	"crypto/rand"
	"testing"
)

func BenchmarkIGEEncrypt(b *testing.B) {
	key := make([]byte, aes256KeySize)
	iv := make([]byte, igeIVSize)
	if _, err := rand.Read(key); err != nil {
		b.Fatalf("rand key: %v", err)
	}
	if _, err := rand.Read(iv); err != nil {
		b.Fatalf("rand iv: %v", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		b.Fatalf("new cipher: %v", err)
	}

	data := make([]byte, 1<<20)
	out := make([]byte, len(data))
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EncryptIGE(out, data, block, iv)
	}
}

func BenchmarkIGEDecrypt(b *testing.B) {
	key := make([]byte, aes256KeySize)
	iv := make([]byte, igeIVSize)
	if _, err := rand.Read(key); err != nil {
		b.Fatalf("rand key: %v", err)
	}
	if _, err := rand.Read(iv); err != nil {
		b.Fatalf("rand iv: %v", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		b.Fatalf("new cipher: %v", err)
	}

	data := make([]byte, 1<<20)
	ciphertext := make([]byte, len(data))
	EncryptIGE(ciphertext, data, block, iv)

	out := make([]byte, len(data))
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecryptIGE(out, ciphertext, block, iv)
	}
}
