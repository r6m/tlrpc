package crypto

import (
	"crypto/sha1"
	"encoding/binary"
	"sync"
	"testing"
)

func TestAuthKeyID(t *testing.T) {
	var key AuthKey
	for i := 0; i < len(key); i++ {
		key[i] = byte(i)
	}

	hash := sha1.Sum(key[:])
	expected := KeyID(binary.LittleEndian.Uint64(hash[:8]))
	if key.ID() != expected {
		t.Fatalf("key id mismatch")
	}
}

func TestAuthKeyEqual(t *testing.T) {
	var a AuthKey
	var b AuthKey
	for i := 0; i < len(a); i++ {
		a[i] = byte(i)
		b[i] = byte(i)
	}
	if !a.Equal(b) {
		t.Fatalf("expected keys to be equal")
	}
	b[0] ^= 0xFF
	if a.Equal(b) {
		t.Fatalf("expected keys to be different")
	}
}

func TestMemoryAuthKeyManager(t *testing.T) {
	manager := NewMemoryAuthKeyManager()
	var key AuthKey
	key[0] = 0x01
	id := key.ID()

	if _, err := manager.Get(id); err != ErrAuthKeyNotFound {
		t.Fatalf("expected not found error")
	}
	if err := manager.Put(id, key); err != nil {
		t.Fatalf("put error: %v", err)
	}
	got, err := manager.Get(id)
	if err != nil {
		t.Fatalf("get error: %v", err)
	}
	if !got.Equal(key) {
		t.Fatalf("stored key mismatch")
	}
	if err := manager.Delete(id); err != nil {
		t.Fatalf("delete error: %v", err)
	}
	if _, err := manager.Get(id); err != ErrAuthKeyNotFound {
		t.Fatalf("expected not found after delete")
	}
}

func TestMemoryAuthKeyManagerConcurrent(t *testing.T) {
	manager := NewMemoryAuthKeyManager()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var key AuthKey
			key[0] = byte(i)
			id := KeyID(i)
			_ = manager.Put(id, key)
			_, _ = manager.Get(id)
			_ = manager.Delete(id)
		}(i)
	}

	wg.Wait()
}
