package tlrpc

import (
	"context"
	"testing"

	"github.com/r6m/tlrpc/crypto"
	runtimev2 "github.com/r6m/tlrpc/internal/runtime"
	"github.com/r6m/tlrpc/session"
)

type recordingRuntimeSender struct{ pushes int }

func (s *recordingRuntimeSender) Push(context.Context, []byte) error {
	s.pushes++
	return nil
}

func TestRuntimePushRegistryTracksBindingAndConnectionSubscription(t *testing.T) {
	var bound []Binding
	var unbound []Binding
	server := &Server{
		onSessionBound:   func(binding Binding, _ Sender) { bound = append(bound, binding) },
		onSessionUnbound: func(binding Binding) { unbound = append(unbound, binding) },
	}
	registry := newRuntimePushRegistry(server)
	sender := &recordingRuntimeSender{}
	snapshot := session.Snapshot{AuthKeyID: crypto.KeyID(11), SessionID: 22, Layer: 228}

	registry.Update(snapshot, sender, true)
	snapshot.UserID = 33
	registry.Update(snapshot, sender, true)
	if len(bound) != 2 || bound[1].UserID != 33 {
		t.Fatalf("bound hooks = %#v", bound)
	}

	registry.Update(snapshot, sender, false)
	if len(bound) != 2 {
		t.Fatalf("subscription change emitted binding hook: %#v", bound)
	}
	if err := registry.Publish(context.Background(), 33, []byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	if sender.pushes != 0 {
		t.Fatalf("unsubscribed sender received %d pushes", sender.pushes)
	}

	registry.Update(snapshot, sender, true)
	if err := registry.Publish(context.Background(), 33, []byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	if sender.pushes != 1 {
		t.Fatalf("subscribed sender received %d pushes, want 1", sender.pushes)
	}

	registry.Remove(snapshot.Key(), sender)
	if len(unbound) != 1 || unbound[0].UserID != 33 {
		t.Fatalf("unbound hooks = %#v", unbound)
	}
}

var _ runtimev2.Sender = (*recordingRuntimeSender)(nil)
