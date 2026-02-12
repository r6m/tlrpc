package session

import (
	"testing"
	"time"

	"github.com/r6m/tlrpc/crypto"
)

func TestMemoryManagerCRUD(t *testing.T) {
	manager := NewMemoryManager()
	keyID := crypto.KeyID(1)

	if _, err := manager.Get(keyID); err != ErrSessionNotFound {
		t.Fatalf("expected not found")
	}

	session, err := manager.Create(keyID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if session.AuthKeyID != keyID {
		t.Fatalf("auth key mismatch")
	}

	loaded, err := manager.Get(keyID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded != session {
		t.Fatalf("session mismatch")
	}

	if err := manager.Delete(keyID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := manager.Get(keyID); err != ErrSessionNotFound {
		t.Fatalf("expected not found after delete")
	}
}

func TestMemoryManagerGC(t *testing.T) {
	manager := NewMemoryManager()
	keyID := crypto.KeyID(2)
	_, err := manager.Create(keyID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	session, err := manager.Get(keyID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	session.LastActivity = time.Now().Add(-2 * time.Hour)
	manager.GC(1 * time.Hour)
	if _, err := manager.Get(keyID); err != ErrSessionNotFound {
		t.Fatalf("expected session GC")
	}
}

func TestLRUCache(t *testing.T) {
	cache := NewLRUCache(2)
	cache.Add(1)
	cache.Add(2)
	if !cache.Contains(1) {
		t.Fatalf("expected to contain 1")
	}
	cache.Add(3)
	if cache.Contains(2) {
		t.Fatalf("expected 2 to be evicted")
	}
	if !cache.Contains(1) || !cache.Contains(3) {
		t.Fatalf("expected 1 and 3 to remain")
	}
}
