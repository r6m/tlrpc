package tlrpc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/r6m/tlrpc/crypto"
	runtimev2 "github.com/r6m/tlrpc/internal/runtime"
	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/session"
)

type recordingRuntimeSender struct {
	pushes       int
	err          error
	lastContext  context.Context
	connectionID uint64
	lastBody     []byte
}

type blockingRuntimeSender struct {
	started chan struct{}
	release chan struct{}
}

type layerRecordingPushObject struct {
	layer *int
}

func (*layerRecordingPushObject) ConstructorID() uint32 { return 0x01020304 }

func (o *layerRecordingPushObject) SerializeTL(w io.Writer) error {
	*o.layer = mtproto.TLLayer(w)
	return mtproto.WriteInt32(w, int32(*o.layer))
}

func (s *blockingRuntimeSender) Push(ctx context.Context, _ []byte) error {
	close(s.started)
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *recordingRuntimeSender) ConnectionID() uint64 { return s.connectionID }

func (s *recordingRuntimeSender) Push(ctx context.Context, body []byte) error {
	s.pushes++
	s.lastContext = ctx
	s.lastBody = append([]byte(nil), body...)
	return s.err
}

func TestRuntimePushRegistryTracksBindingAndConnectionSubscription(t *testing.T) {
	observer := newRecordingObserver()
	server := NewServer(WithObserver(observer))
	t.Cleanup(func() { _ = server.Stop() })
	registry := newRuntimePushRegistry(server)
	sender := &recordingRuntimeSender{connectionID: 44}
	snapshot := session.Snapshot{AuthKeyID: crypto.KeyID(11), SessionID: 22, Layer: 228}

	registry.Update(snapshot, 1, sender, true)
	firstBound := waitSessionEvent(t, observer)
	snapshot.UserID = 33
	registry.Update(snapshot, 1, sender, true)
	secondBound := waitSessionEvent(t, observer)
	if firstBound.ConnectionID != 44 || secondBound.ConnectionID != 44 || secondBound.AuthKeyID != 11 {
		t.Fatalf("bound events = %#v, %#v", firstBound, secondBound)
	}

	registry.Update(snapshot, 1, sender, false)
	if err := registry.Publish(context.Background(), 33, []byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	if sender.pushes != 0 {
		t.Fatalf("unsubscribed sender received %d pushes", sender.pushes)
	}

	registry.Update(snapshot, 1, sender, true)
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

	registry.Update(session.Snapshot{AuthKeyID: 11, SessionID: 22, UserID: 33}, 1, first, true)
	registry.Update(session.Snapshot{AuthKeyID: 11, SessionID: 23, UserID: 33}, 1, sameAuthKey, true)
	registry.Update(session.Snapshot{AuthKeyID: 12, SessionID: 22, UserID: 33}, 1, sameSessionID, true)

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

func TestRuntimePushRegistryPublishExceptAuthKeySkipsEveryAuthorizationSession(t *testing.T) {
	registry := newRuntimePushRegistry(nil)
	first := &recordingRuntimeSender{}
	sameAuthKey := &recordingRuntimeSender{}
	otherAuthKey := &recordingRuntimeSender{}

	registry.Update(session.Snapshot{AuthKeyID: 11, SessionID: 22, UserID: 33}, 1, first, true)
	registry.Update(session.Snapshot{AuthKeyID: 11, SessionID: 23, UserID: 33}, 1, sameAuthKey, true)
	registry.Update(session.Snapshot{AuthKeyID: 12, SessionID: 22, UserID: 33}, 1, otherAuthKey, true)

	if err := registry.PublishExceptAuthKey(context.Background(), 33, 11, []byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	if first.pushes != 0 || sameAuthKey.pushes != 0 || otherAuthKey.pushes != 1 {
		t.Fatalf("auth-key-excluded publish counts = (%d, %d, %d), want (0, 0, 1)", first.pushes, sameAuthKey.pushes, otherAuthKey.pushes)
	}
}

func TestRuntimePushRegistryColdParallelConnectionDoesNotMaskSubscriber(t *testing.T) {
	registry := newRuntimePushRegistry(nil)
	subscribed := &recordingRuntimeSender{}
	cold := &recordingRuntimeSender{}
	snapshot := session.Snapshot{AuthKeyID: 11, SessionID: 22, UserID: 33}

	registry.Update(snapshot, 1, subscribed, true)
	registry.Update(snapshot, 1, cold, false)
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

	registry.Update(snapshot, 1, cold, true)
	if err := registry.Publish(context.Background(), 33, []byte{3}); err != nil {
		t.Fatal(err)
	}
	if subscribed.pushes != 3 || cold.pushes != 1 {
		t.Fatalf("two-subscriber publish counts = (%d, %d), want (3, 1)", subscribed.pushes, cold.pushes)
	}
}

func TestRuntimePushRegistryActiveUserIDsReturnsSortedDetachedPositiveSnapshot(t *testing.T) {
	registry := newRuntimePushRegistry(nil)
	registry.byUser[-4] = map[session.SessionKey][]*runtimePushBinding{}
	registry.byUser[0] = map[session.SessionKey][]*runtimePushBinding{}
	registry.byUser[9] = map[session.SessionKey][]*runtimePushBinding{}
	registry.byUser[3] = map[session.SessionKey][]*runtimePushBinding{}
	registry.byUser[7] = map[session.SessionKey][]*runtimePushBinding{}

	got := registry.ActiveUserIDs()
	want := []int64{3, 7, 9}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ActiveUserIDs() = %v, want %v", got, want)
	}

	got[0] = 999
	again := registry.ActiveUserIDs()
	if !reflect.DeepEqual(again, want) {
		t.Fatalf("ActiveUserIDs() after caller mutation = %v, want %v", again, want)
	}
}

func TestRuntimePushRegistryActiveUserIDsTracksReachableUsersOnly(t *testing.T) {
	registry := newRuntimePushRegistry(nil)
	online := &recordingRuntimeSender{}
	offline := &recordingRuntimeSender{}

	registry.Update(session.Snapshot{AuthKeyID: 11, SessionID: 21, UserID: 31}, 1, online, true)
	registry.Update(session.Snapshot{AuthKeyID: 12, SessionID: 22, UserID: 0}, 1, offline, true)
	registry.Update(session.Snapshot{AuthKeyID: 13, SessionID: 23, UserID: 41}, 1, offline, false)

	want := []int64{31}
	if got := registry.ActiveUserIDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ActiveUserIDs() = %v, want %v", got, want)
	}

	registry.Remove(session.SessionKey{AuthKeyID: 11, SessionID: 21}, online)
	if got := registry.ActiveUserIDs(); len(got) != 0 {
		t.Fatalf("ActiveUserIDs() after removal = %v, want empty", got)
	}
}

func TestServerActiveUserIDsIsNilSafe(t *testing.T) {
	var nilServer *Server
	if got := nilServer.ActiveUserIDs(); got != nil {
		t.Fatalf("nil server ActiveUserIDs() = %v, want nil", got)
	}

	server := &Server{}
	if got := server.ActiveUserIDs(); got != nil {
		t.Fatalf("server without runtime pushes ActiveUserIDs() = %v, want nil", got)
	}

	live := NewServer()
	live.runtimePushes.Update(session.Snapshot{AuthKeyID: 51, SessionID: 61, UserID: 71}, 1, &recordingRuntimeSender{}, true)
	got := live.ActiveUserIDs()
	want := []int64{71}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("server ActiveUserIDs() = %v, want %v", got, want)
	}
}

func TestServerPublishExceptUsesCompositeBindingAndAggregatesIncludedFailures(t *testing.T) {
	server := NewServer()
	excludedFailure := errors.New("excluded failure")
	includedFailure := errors.New("included failure")
	excluded := &recordingRuntimeSender{err: excludedFailure}
	included := &recordingRuntimeSender{err: includedFailure}

	server.runtimePushes.Update(session.Snapshot{AuthKeyID: 41, SessionID: 51, UserID: 61}, 1, excluded, true)
	server.runtimePushes.Update(session.Snapshot{AuthKeyID: 41, SessionID: 52, UserID: 61}, 1, included, true)

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

func TestServerPublishExceptAuthKeyPreservesOtherAuthorizations(t *testing.T) {
	server := NewServer()
	first := &recordingRuntimeSender{}
	sameAuthKey := &recordingRuntimeSender{}
	otherAuthKey := &recordingRuntimeSender{}

	server.runtimePushes.Update(session.Snapshot{AuthKeyID: 41, SessionID: 51, UserID: 61}, 1, first, true)
	server.runtimePushes.Update(session.Snapshot{AuthKeyID: 41, SessionID: 52, UserID: 61}, 1, sameAuthKey, true)
	server.runtimePushes.Update(session.Snapshot{AuthKeyID: 42, SessionID: 51, UserID: 61}, 1, otherAuthKey, true)

	if err := server.PublishExceptAuthKey(61, 41, &runtimeApplicationTestResponse{Value: "message update"}); err != nil {
		t.Fatal(err)
	}
	if first.pushes != 0 || sameAuthKey.pushes != 0 || otherAuthKey.pushes != 1 {
		t.Fatalf("auth-key-excluded server publish counts = (%d, %d, %d), want (0, 0, 1)", first.pushes, sameAuthKey.pushes, otherAuthKey.pushes)
	}
}

func TestServerPublishProjectedUsesExactSessionLayerAndLeaseGeneration(t *testing.T) {
	server := NewServer()
	first := &recordingRuntimeSender{}
	second := &recordingRuntimeSender{}
	server.runtimePushes.Update(session.Snapshot{AuthKeyID: 41, SessionID: 51, UserID: 61, Layer: 228}, 7, first, true)
	server.runtimePushes.Update(session.Snapshot{AuthKeyID: 42, SessionID: 52, UserID: 61, Layer: 229}, 9, second, true)

	seen := make(map[int64]Binding)
	err := server.PublishProjected(61, func(_ context.Context, binding Binding) (TLObject, error) {
		seen[binding.SessionID] = binding
		value := "layer-228"
		if binding.Layer == 229 {
			value = "layer-229"
		}
		return &runtimeApplicationTestResponse{Value: value}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen[51].Layer != 228 || seen[51].LeaseGeneration != 7 || seen[52].Layer != 229 || seen[52].LeaseGeneration != 9 {
		t.Fatalf("projected bindings = %#v", seen)
	}
	for name, test := range map[string]struct {
		sender *recordingRuntimeSender
		value  string
	}{
		"layer 228": {sender: first, value: "layer-228"},
		"layer 229": {sender: second, value: "layer-229"},
	} {
		response := &runtimeApplicationTestResponse{}
		if err := response.DeserializeTL(bytes.NewReader(test.sender.lastBody)); err != nil {
			t.Fatalf("%s decode: %v", name, err)
		}
		if response.Value != test.value {
			t.Fatalf("%s value = %q, want %q", name, response.Value, test.value)
		}
	}
}

func TestServerPublishProjectedUsesSchemaLayerForZeroLayerRegistration(t *testing.T) {
	server := NewServer()
	server.schemaLayer = 229
	server.schemaLayerSet = true
	sender := &recordingRuntimeSender{}
	snapshot := session.Snapshot{AuthKeyID: 41, SessionID: 51, UserID: 61}

	for _, test := range []struct {
		snapshotLayer int
		wantLayer     int
	}{{snapshotLayer: 0, wantLayer: 229}, {snapshotLayer: 228, wantLayer: 228}, {snapshotLayer: 229, wantLayer: 229}} {
		snapshot.Layer = test.snapshotLayer
		server.runtimePushes.Update(snapshot, 7, sender, true)
		encodedLayer := 0
		var projectedBinding Binding
		err := server.PublishProjected(61, func(_ context.Context, binding Binding) (TLObject, error) {
			projectedBinding = binding
			return &layerRecordingPushObject{layer: &encodedLayer}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if projectedBinding.Layer != test.wantLayer || projectedBinding.LeaseGeneration != 7 {
			t.Fatalf("projected binding = %#v, want layer %d generation 7", projectedBinding, test.wantLayer)
		}
		if encodedLayer != test.wantLayer {
			t.Fatalf("encoded layer = %d, want %d", encodedLayer, test.wantLayer)
		}
	}
}

func TestRuntimePushRegistryWithoutServerPreservesZeroLayerBinding(t *testing.T) {
	registry := newRuntimePushRegistry(nil)
	sender := &recordingRuntimeSender{}
	snapshot := session.Snapshot{AuthKeyID: 41, SessionID: 51, UserID: 61}

	registry.Update(snapshot, 7, sender, true)
	registry.Update(snapshot, 7, sender, true)

	registry.mu.RLock()
	targets := append([]*runtimePushBinding(nil), registry.byKey[snapshot.Key()]...)
	registry.mu.RUnlock()
	if len(targets) != 1 {
		t.Fatalf("registered targets = %d, want 1", len(targets))
	}
	targets[0].mu.Lock()
	binding := targets[0].binding
	targets[0].mu.Unlock()
	if binding.Layer != 0 || binding.LeaseGeneration != 7 {
		t.Fatalf("standalone registry binding = %#v, want zero layer generation 7", binding)
	}
}

func TestServerPublishProjectedFailsClosedAndPropagatesProjectionError(t *testing.T) {
	server := NewServer()
	projectionFailure := errors.New("unsupported layer projection")
	unsupported := &recordingRuntimeSender{}
	supported := &recordingRuntimeSender{}
	server.runtimePushes.Update(session.Snapshot{AuthKeyID: 41, SessionID: 51, UserID: 61, Layer: 228}, 3, unsupported, true)
	server.runtimePushes.Update(session.Snapshot{AuthKeyID: 42, SessionID: 52, UserID: 61, Layer: 229}, 4, supported, true)

	err := server.PublishProjected(61, func(_ context.Context, binding Binding) (TLObject, error) {
		if binding.Layer == 228 {
			return nil, projectionFailure
		}
		return &runtimeApplicationTestResponse{Value: "supported"}, nil
	})
	if !errors.Is(err, projectionFailure) {
		t.Fatalf("PublishProjected error = %v, want projection failure", err)
	}
	if unsupported.pushes != 0 || supported.pushes != 1 {
		t.Fatalf("projected push counts = (%d, %d), want (0, 1)", unsupported.pushes, supported.pushes)
	}
}

func TestServerPublishProjectedRejectsMidProjectionLayerDrift(t *testing.T) {
	server := NewServer()
	sender := &recordingRuntimeSender{}
	snapshot := session.Snapshot{AuthKeyID: 41, SessionID: 51, UserID: 61, Layer: 228}
	server.runtimePushes.Update(snapshot, 6, sender, true)

	err := server.PublishProjected(61, func(_ context.Context, binding Binding) (TLObject, error) {
		if binding.Layer != 228 || binding.LeaseGeneration != 6 {
			t.Fatalf("initial binding = %#v", binding)
		}
		snapshot.Layer = 229
		server.runtimePushes.Update(snapshot, 6, sender, true)
		return &runtimeApplicationTestResponse{Value: "old-layer bytes"}, nil
	})
	if !errors.Is(err, ErrPushBindingChanged) {
		t.Fatalf("PublishProjected error = %v, want ErrPushBindingChanged", err)
	}
	if sender.pushes != 0 {
		t.Fatalf("drifted binding received %d pushes, want 0", sender.pushes)
	}
}

func TestServerPublishProjectedRejectsReconnectGenerationAndLayerReplacement(t *testing.T) {
	server := NewServer()
	key := session.SessionKey{AuthKeyID: 41, SessionID: 51}
	oldSender := &recordingRuntimeSender{}
	newSender := &recordingRuntimeSender{}
	server.runtimePushes.Update(session.Snapshot{AuthKeyID: key.AuthKeyID, SessionID: key.SessionID, UserID: 61, Layer: 228}, 5, oldSender, true)

	err := server.PublishProjected(61, func(_ context.Context, binding Binding) (TLObject, error) {
		if binding.Layer != 228 || binding.LeaseGeneration != 5 {
			t.Fatalf("old binding = %#v", binding)
		}
		server.runtimePushes.Remove(key, oldSender)
		server.runtimePushes.Update(session.Snapshot{AuthKeyID: key.AuthKeyID, SessionID: key.SessionID, UserID: 61, Layer: 229}, 6, newSender, true)
		return &runtimeApplicationTestResponse{Value: "layer-228 bytes"}, nil
	})
	if !errors.Is(err, ErrPushBindingChanged) {
		t.Fatalf("PublishProjected error = %v, want ErrPushBindingChanged", err)
	}
	if oldSender.pushes != 0 || newSender.pushes != 0 {
		t.Fatalf("replaced-session push counts = (%d, %d), want (0, 0)", oldSender.pushes, newSender.pushes)
	}
}

func TestServerPublishProjectedExceptPreservesOriginRules(t *testing.T) {
	server := NewServer()
	exactOrigin := &recordingRuntimeSender{}
	sameAuthKey := &recordingRuntimeSender{}
	otherAuthKey := &recordingRuntimeSender{}
	server.runtimePushes.Update(session.Snapshot{AuthKeyID: 41, SessionID: 51, UserID: 61, Layer: 228}, 1, exactOrigin, true)
	server.runtimePushes.Update(session.Snapshot{AuthKeyID: 41, SessionID: 52, UserID: 61, Layer: 229}, 1, sameAuthKey, true)
	server.runtimePushes.Update(session.Snapshot{AuthKeyID: 42, SessionID: 53, UserID: 61, Layer: 229}, 1, otherAuthKey, true)
	project := func(_ context.Context, _ Binding) (TLObject, error) {
		return &runtimeApplicationTestResponse{Value: "update"}, nil
	}

	if err := server.PublishProjectedExcept(61, Binding{AuthKeyID: 41, SessionID: 51}, project); err != nil {
		t.Fatal(err)
	}
	if exactOrigin.pushes != 0 || sameAuthKey.pushes != 1 || otherAuthKey.pushes != 1 {
		t.Fatalf("exact-origin counts = (%d, %d, %d), want (0, 1, 1)", exactOrigin.pushes, sameAuthKey.pushes, otherAuthKey.pushes)
	}
	if err := server.PublishProjectedExceptAuthKey(61, 41, project); err != nil {
		t.Fatal(err)
	}
	if exactOrigin.pushes != 0 || sameAuthKey.pushes != 1 || otherAuthKey.pushes != 2 {
		t.Fatalf("auth-key-origin counts = (%d, %d, %d), want (0, 1, 2)", exactOrigin.pushes, sameAuthKey.pushes, otherAuthKey.pushes)
	}
}

func TestProjectedPushDoesNotBlockUnrelatedRegistryUpdates(t *testing.T) {
	server := NewServer()
	blocked := &blockingRuntimeSender{started: make(chan struct{}), release: make(chan struct{})}
	server.runtimePushes.Update(session.Snapshot{AuthKeyID: 41, SessionID: 51, UserID: 61, Layer: 228}, 1, blocked, true)
	published := make(chan error, 1)
	go func() {
		published <- server.PublishProjected(61, func(context.Context, Binding) (TLObject, error) {
			return &runtimeApplicationTestResponse{Value: "blocked"}, nil
		})
	}()
	select {
	case <-blocked.started:
	case <-time.After(time.Second):
		t.Fatal("projected sender did not start")
	}
	sameTargetUpdated := make(chan struct{})
	go func() {
		server.runtimePushes.Update(session.Snapshot{AuthKeyID: 41, SessionID: 51, UserID: 61, Layer: 229}, 1, blocked, true)
		close(sameTargetUpdated)
	}()
	select {
	case <-sameTargetUpdated:
		t.Fatal("same-target update completed before projected push")
	case <-time.After(50 * time.Millisecond):
	}

	unrelated := &recordingRuntimeSender{}
	updated := make(chan struct{})
	go func() {
		server.runtimePushes.Update(session.Snapshot{AuthKeyID: 42, SessionID: 52, UserID: 62, Layer: 229}, 1, unrelated, true)
		server.runtimePushes.Remove(session.SessionKey{AuthKeyID: 42, SessionID: 52}, unrelated)
		close(updated)
	}()
	select {
	case <-updated:
	case <-time.After(time.Second):
		t.Fatal("unrelated registry update blocked behind projected push")
	}
	close(blocked.release)
	if err := <-published; err != nil {
		t.Fatal(err)
	}
	select {
	case <-sameTargetUpdated:
	case <-time.After(time.Second):
		t.Fatal("same-target update did not resume after projected push")
	}
}

func TestProjectedPushSerializesSameSessionLayerUpdateThroughAcceptance(t *testing.T) {
	server := NewServer()
	blocked := &blockingRuntimeSender{started: make(chan struct{}), release: make(chan struct{})}
	snapshot := session.Snapshot{AuthKeyID: 41, SessionID: 51, UserID: 61, Layer: 228}
	server.runtimePushes.Update(snapshot, 5, blocked, true)
	published := make(chan error, 1)
	go func() {
		published <- server.PublishProjected(61, func(_ context.Context, binding Binding) (TLObject, error) {
			if binding.Layer != 228 || binding.LeaseGeneration != 5 {
				t.Errorf("projected binding = %#v", binding)
			}
			return &runtimeApplicationTestResponse{Value: "layer-228"}, nil
		})
	}()
	select {
	case <-blocked.started:
	case <-time.After(time.Second):
		t.Fatal("projected sender did not start")
	}

	updated := make(chan struct{})
	go func() {
		snapshot.Layer = 229
		server.runtimePushes.Update(snapshot, 5, blocked, true)
		close(updated)
	}()
	select {
	case <-updated:
		t.Fatal("same-session layer changed before projected push acceptance")
	case <-time.After(50 * time.Millisecond):
	}
	close(blocked.release)
	if err := <-published; err != nil {
		t.Fatal(err)
	}
	select {
	case <-updated:
	case <-time.After(time.Second):
		t.Fatal("same-session layer update did not resume after push acceptance")
	}
}

var _ runtimev2.Sender = (*recordingRuntimeSender)(nil)
var _ runtimev2.Sender = (*blockingRuntimeSender)(nil)
