package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/session"
)

func TestSessionLeaseReconnectRetiresAndWaitsForPreviousOwner(t *testing.T) {
	store := session.NewMemoryStore()
	registry := NewSessionLeaseRegistry(store)
	key := session.SessionKey{AuthKeyID: crypto.KeyID(7), SessionID: 9}
	initial := session.Snapshot{AuthKeyID: key.AuthKeyID, SessionID: key.SessionID}
	first, err := registry.Acquire(context.Background(), key, initial)
	if err != nil {
		t.Fatalf("acquire first: %v", err)
	}
	if !first.Created() {
		t.Fatal("first lease did not report durable session creation")
	}

	secondResult := make(chan struct {
		lease *SessionLease
		err   error
	}, 1)
	go func() {
		lease, acquireErr := registry.Acquire(context.Background(), key, initial)
		secondResult <- struct {
			lease *SessionLease
			err   error
		}{lease: lease, err: acquireErr}
	}()

	select {
	case <-first.Context().Done():
		if !errors.Is(context.Cause(first.Context()), ErrSessionLeaseReplaced) {
			t.Fatalf("first lease cause = %v", context.Cause(first.Context()))
		}
	case <-time.After(time.Second):
		t.Fatal("reconnect did not retire the first lease")
	}
	select {
	case <-secondResult:
		t.Fatal("replacement acquired before the first owner released")
	default:
	}
	if err := first.Commit(context.Background(), initial); !errors.Is(err, ErrSessionLeaseReplaced) {
		t.Fatalf("retired commit error = %v, want ErrSessionLeaseReplaced", err)
	}

	first.Release()
	var second *SessionLease
	select {
	case result := <-secondResult:
		if result.err != nil {
			t.Fatalf("acquire replacement: %v", result.err)
		}
		second = result.lease
	case <-time.After(time.Second):
		t.Fatal("replacement did not acquire after release")
	}
	if second.Created() {
		t.Fatal("replacement lease incorrectly reported durable session creation")
	}
	t.Cleanup(second.Release)

	next := initial
	next.ServerSeqNo = 4
	if err := second.Commit(context.Background(), next); err != nil {
		t.Fatalf("commit replacement: %v", err)
	}
	stored, err := store.Load(context.Background(), key)
	if err != nil {
		t.Fatalf("load committed snapshot: %v", err)
	}
	if stored.ServerSeqNo != 4 {
		t.Fatalf("stored server seq = %d, want 4", stored.ServerSeqNo)
	}
}

func TestSessionLeaseAcquireHonorsCanceledContext(t *testing.T) {
	store := session.NewMemoryStore()
	registry := NewSessionLeaseRegistry(store)
	key := session.SessionKey{AuthKeyID: crypto.KeyID(7), SessionID: 9}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := registry.Acquire(ctx, key, session.Snapshot{AuthKeyID: key.AuthKeyID, SessionID: key.SessionID}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire() error = %v, want context.Canceled", err)
	}
}
