package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/r6m/tlrpc/crypto"
)

func TestLocalCoordinatorTakeoverCancelsLostOwnerAndWaitsForRelease(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	coordinator := NewLocalCoordinator(store)
	key := SessionKey{AuthKeyID: crypto.KeyID(7), SessionID: 9}
	initial := Snapshot{AuthKeyID: key.AuthKeyID, SessionID: key.SessionID}

	first, err := coordinator.Acquire(ctx, key, initial)
	if err != nil {
		t.Fatalf("acquire first: %v", err)
	}
	if !first.Created() || first.Generation() != 1 {
		t.Fatalf("first lease created/generation = %v/%d, want true/1", first.Created(), first.Generation())
	}

	secondResult := make(chan struct {
		lease Lease
		err   error
	}, 1)
	go func() {
		lease, acquireErr := coordinator.Acquire(ctx, key, initial)
		secondResult <- struct {
			lease Lease
			err   error
		}{lease: lease, err: acquireErr}
	}()

	select {
	case <-first.Context().Done():
		if !errors.Is(context.Cause(first.Context()), ErrLeaseLost) {
			t.Fatalf("first lease cause = %v, want ErrLeaseLost", context.Cause(first.Context()))
		}
	case <-time.After(time.Second):
		t.Fatal("takeover did not cancel the first owner")
	}
	select {
	case <-secondResult:
		t.Fatal("replacement acquired before the first owner released")
	default:
	}
	if err := first.Save(ctx, initial); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("lost-owner save error = %v, want ErrLeaseLost", err)
	}
	if err := first.Delete(ctx); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("lost-owner delete error = %v, want ErrLeaseLost", err)
	}

	first.Release()
	var second Lease
	select {
	case result := <-secondResult:
		if result.err != nil {
			t.Fatalf("acquire replacement: %v", result.err)
		}
		second = result.lease
	case <-time.After(time.Second):
		t.Fatal("replacement did not acquire after release")
	}
	defer second.Release()
	if second.Created() {
		t.Fatal("replacement incorrectly reported durable session creation")
	}
	if second.Generation() != 2 {
		t.Fatalf("replacement generation = %d, want 2", second.Generation())
	}

	next := initial
	next.ServerSeqNo = 4
	if err := second.Save(ctx, next); err != nil {
		t.Fatalf("save replacement: %v", err)
	}
	stored, err := store.Load(ctx, key)
	if err != nil {
		t.Fatalf("load committed snapshot: %v", err)
	}
	if stored.ServerSeqNo != 4 {
		t.Fatalf("stored server seq = %d, want 4", stored.ServerSeqNo)
	}
}

func TestLocalCoordinatorSaveDeleteAreFencedToActiveGeneration(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	coordinator := NewLocalCoordinator(store)
	key := SessionKey{AuthKeyID: crypto.KeyID(7), SessionID: 9}
	initial := Snapshot{AuthKeyID: key.AuthKeyID, SessionID: key.SessionID}

	first, err := coordinator.Acquire(ctx, key, initial)
	if err != nil {
		t.Fatalf("acquire first: %v", err)
	}
	first.Release()
	if err := first.Save(ctx, initial); !errors.Is(err, ErrLeaseInactive) {
		t.Fatalf("released save error = %v, want ErrLeaseInactive", err)
	}

	second, err := coordinator.Acquire(ctx, key, initial)
	if err != nil {
		t.Fatalf("acquire second: %v", err)
	}
	if second.Generation() != 2 {
		t.Fatalf("second generation = %d, want 2", second.Generation())
	}
	if err := second.Delete(ctx); err != nil {
		t.Fatalf("delete active lease: %v", err)
	}
	if _, err := store.Load(ctx, key); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("load after delete error = %v, want ErrSessionNotFound", err)
	}
	if !errors.Is(context.Cause(second.Context()), ErrLeaseReleased) {
		t.Fatalf("deleted lease cause = %v, want ErrLeaseReleased", context.Cause(second.Context()))
	}
	if err := second.Save(ctx, initial); !errors.Is(err, ErrLeaseInactive) {
		t.Fatalf("deleted save error = %v, want ErrLeaseInactive", err)
	}
	second.Release()

	third, err := coordinator.Acquire(ctx, key, initial)
	if err != nil {
		t.Fatalf("acquire third: %v", err)
	}
	defer third.Release()
	if !third.Created() || third.Generation() != 3 {
		t.Fatalf("third created/generation = %v/%d, want true/3", third.Created(), third.Generation())
	}
}

func TestLocalCoordinatorAcquireHonorsContextAndSnapshotIdentity(t *testing.T) {
	coordinator := NewLocalCoordinator(NewMemoryStore())
	key := SessionKey{AuthKeyID: crypto.KeyID(7), SessionID: 9}
	if _, err := coordinator.Acquire(context.Background(), key, Snapshot{}); !errors.Is(err, ErrSessionKeyMismatch) {
		t.Fatalf("identity error = %v, want ErrSessionKeyMismatch", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := coordinator.Acquire(ctx, key, Snapshot{AuthKeyID: key.AuthKeyID, SessionID: key.SessionID}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled acquire error = %v, want context.Canceled", err)
	}
}
