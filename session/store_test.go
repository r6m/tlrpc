package session

import (
	"context"
	"errors"
	"testing"

	"github.com/r6m/tlrpc/crypto"
)

func TestMemoryStoreUsesDetachedSnapshotsAndAtomicLoadOrCreate(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	key := SessionKey{AuthKeyID: crypto.KeyID(7), SessionID: 9}
	initial := Snapshot{
		AuthKeyID: key.AuthKeyID, SessionID: key.SessionID, ServerSeqNo: 3, PushSubscription: true,
		ClientMsgIDFloor: 1, RecentClientMsgIDs: []int64{5}, RecentClientSeqNos: []int32{1},
	}

	created, wasCreated, err := store.LoadOrCreate(ctx, key, initial)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !wasCreated {
		t.Fatal("initial LoadOrCreate did not report creation")
	}
	created.ServerSeqNo = 99
	created.RecentClientMsgIDs[0] = 99
	created.RecentClientSeqNos[0] = 99

	existing, wasCreated, err := store.LoadOrCreate(ctx, key, Snapshot{AuthKeyID: key.AuthKeyID, SessionID: key.SessionID, ServerSeqNo: 100})
	if err != nil {
		t.Fatalf("load existing: %v", err)
	}
	if wasCreated {
		t.Fatal("existing LoadOrCreate incorrectly reported creation")
	}
	if existing.ServerSeqNo != 3 || !existing.PushSubscription || existing.ClientMsgIDFloor != 1 || existing.RecentClientMsgIDs[0] != 5 || existing.RecentClientSeqNos[0] != 1 {
		t.Fatalf("stored snapshot was aliased or reset: %+v", existing)
	}

	existing.ServerSeqNo = 4
	if err := store.Save(ctx, key, existing); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := store.Load(ctx, key)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.ServerSeqNo != 4 {
		t.Fatalf("loaded server seq = %d, want 4", loaded.ServerSeqNo)
	}
}

func TestMemoryStoreValidatesIdentityAndContext(t *testing.T) {
	store := NewMemoryStore()
	key := SessionKey{AuthKeyID: crypto.KeyID(7), SessionID: 9}
	if _, _, err := store.LoadOrCreate(context.Background(), key, Snapshot{}); !errors.Is(err, ErrSessionKeyMismatch) {
		t.Fatalf("identity error = %v, want ErrSessionKeyMismatch", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Load(canceled, key); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled load error = %v, want context.Canceled", err)
	}
}
