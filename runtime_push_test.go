package tlrpc

import (
	"context"
	"errors"
	"testing"

	"github.com/r6m/tlrpc/crypto"
	runtimev2 "github.com/r6m/tlrpc/internal/runtime"
	"github.com/r6m/tlrpc/session"
)

type recordingRuntimeSender struct {
	pushes       int
	err          error
	lastContext  context.Context
	connectionID uint64
}

func (s *recordingRuntimeSender) ConnectionID() uint64 { return s.connectionID }

func (s *recordingRuntimeSender) Push(ctx context.Context, _ []byte) error {
	s.pushes++
	s.lastContext = ctx
	return s.err
}

func TestRuntimePushRegistryTracksBindingAndConnectionSubscription(t *testing.T) {
	observer := newRecordingObserver()
	server := NewServer(WithObserver(observer))
	t.Cleanup(func() { _ = server.Stop() })
	registry := newRuntimePushRegistry(server)
	sender := &recordingRuntimeSender{connectionID: 44}
	snapshot := session.Snapshot{AuthKeyID: crypto.KeyID(11), SessionID: 22, Layer: 228}

	registry.Update(snapshot, sender, true)
	firstBound := waitSessionEvent(t, observer)
	snapshot.UserID = 33
	registry.Update(snapshot, sender, true)
	secondBound := waitSessionEvent(t, observer)
	if firstBound.ConnectionID != 44 || secondBound.ConnectionID != 44 || secondBound.AuthKeyID != 11 {
		t.Fatalf("bound events = %#v, %#v", firstBound, secondBound)
	}

	registry.Update(snapshot, sender, false)
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
	unbound := waitSessionEvent(t, observer)
	if unbound.Action != "released" || unbound.ConnectionID != 44 {
		t.Fatalf("unbound event = %#v", unbound)
	}
}

func TestRuntimePushRegistryPublishExceptSkipsOnlyExactSession(t *testing.T) {
	registry := newRuntimePushRegistry(nil)
	first := &recordingRuntimeSender{}
	sameAuthKey := &recordingRuntimeSender{}
	sameSessionID := &recordingRuntimeSender{}

	registry.Update(session.Snapshot{AuthKeyID: 11, SessionID: 22, UserID: 33}, first, true)
	registry.Update(session.Snapshot{AuthKeyID: 11, SessionID: 23, UserID: 33}, sameAuthKey, true)
	registry.Update(session.Snapshot{AuthKeyID: 12, SessionID: 22, UserID: 33}, sameSessionID, true)

	excluded := session.SessionKey{AuthKeyID: 11, SessionID: 22}
	if err := registry.PublishExcept(context.Background(), 33, excluded, []byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	if first.pushes != 0 || sameAuthKey.pushes != 1 || sameSessionID.pushes != 1 {
		t.Fatalf("excluded publish counts = (%d, %d, %d), want (0, 1, 1)", first.pushes, sameAuthKey.pushes, sameSessionID.pushes)
	}

	if err := registry.Publish(context.Background(), 33, []byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	if first.pushes != 1 || sameAuthKey.pushes != 2 || sameSessionID.pushes != 2 {
		t.Fatalf("ordinary publish counts = (%d, %d, %d), want (1, 2, 2)", first.pushes, sameAuthKey.pushes, sameSessionID.pushes)
	}
}

func TestRuntimePushRegistryColdParallelConnectionDoesNotMaskSubscriber(t *testing.T) {
	registry := newRuntimePushRegistry(nil)
	subscribed := &recordingRuntimeSender{}
	cold := &recordingRuntimeSender{}
	snapshot := session.Snapshot{AuthKeyID: 11, SessionID: 22, UserID: 33}

	registry.Update(snapshot, subscribed, true)
	registry.Update(snapshot, cold, false)
	if err := registry.Publish(context.Background(), 33, []byte{1}); err != nil {
		t.Fatal(err)
	}
	if subscribed.pushes != 1 || cold.pushes != 0 {
		t.Fatalf("parallel publish counts = (%d, %d), want (1, 0)", subscribed.pushes, cold.pushes)
	}

	registry.Remove(snapshot.Key(), cold)
	if err := registry.Publish(context.Background(), 33, []byte{2}); err != nil {
		t.Fatal(err)
	}
	if subscribed.pushes != 2 || cold.pushes != 0 {
		t.Fatalf("publish after cold removal = (%d, %d), want (2, 0)", subscribed.pushes, cold.pushes)
	}

	registry.Update(snapshot, cold, true)
	if err := registry.Publish(context.Background(), 33, []byte{3}); err != nil {
		t.Fatal(err)
	}
	if subscribed.pushes != 3 || cold.pushes != 1 {
		t.Fatalf("two-subscriber publish counts = (%d, %d), want (3, 1)", subscribed.pushes, cold.pushes)
	}
}

func TestServerPublishExceptUsesCompositeBindingAndAggregatesIncludedFailures(t *testing.T) {
	server := NewServer()
	excludedFailure := errors.New("excluded failure")
	includedFailure := errors.New("included failure")
	excluded := &recordingRuntimeSender{err: excludedFailure}
	included := &recordingRuntimeSender{err: includedFailure}

	server.runtimePushes.Update(session.Snapshot{AuthKeyID: 41, SessionID: 51, UserID: 61}, excluded, true)
	server.runtimePushes.Update(session.Snapshot{AuthKeyID: 41, SessionID: 52, UserID: 61}, included, true)

	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "publish deadline scope")
	err := server.PublishExceptContext(ctx, 61, Binding{
		AuthKeyID: 41,
		SessionID: 51,
		UserID:    999,
		Layer:     1,
	}, &runtimeApplicationTestResponse{Value: "draft update"})
	if !errors.Is(err, includedFailure) {
		t.Fatalf("PublishExceptContext error = %v, want included failure", err)
	}
	if errors.Is(err, excludedFailure) {
		t.Fatalf("PublishExceptContext included excluded-session failure: %v", err)
	}
	if excluded.pushes != 0 || included.pushes != 1 {
		t.Fatalf("publish counts = (%d, %d), want (0, 1)", excluded.pushes, included.pushes)
	}
	if got := included.lastContext.Value(contextKey{}); got != "publish deadline scope" {
		t.Fatalf("included sender context value = %v", got)
	}

	included.err = nil
	if err := server.PublishExcept(61, Binding{AuthKeyID: 41, SessionID: 51}, &runtimeApplicationTestResponse{Value: "next update"}); err != nil {
		t.Fatal(err)
	}
	if excluded.pushes != 0 || included.pushes != 2 {
		t.Fatalf("non-context publish counts = (%d, %d), want (0, 2)", excluded.pushes, included.pushes)
	}
}

var _ runtimev2.Sender = (*recordingRuntimeSender)(nil)
