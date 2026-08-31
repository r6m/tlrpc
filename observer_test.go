package tlrpc

import (
	"context"
	"testing"
	"time"

	"github.com/r6m/tlrpc/session"
)

type recordingObserver struct {
	events chan Event
}

func newRecordingObserver() *recordingObserver {
	return &recordingObserver{events: make(chan Event, 64)}
}

func (o *recordingObserver) ObserveTLRPC(event Event) {
	o.events <- event
}

func waitRPCEvent(t *testing.T, observer *recordingObserver) RPCEvent {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case raw := <-observer.events:
			if typed, ok := raw.(RPCEvent); ok {
				return typed
			}
		case <-deadline:
			t.Fatal("timed out waiting for RPCEvent")
		}
	}
}

func waitStoreEvent(t *testing.T, observer *recordingObserver) StoreEvent {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case raw := <-observer.events:
			if typed, ok := raw.(StoreEvent); ok {
				return typed
			}
		case <-deadline:
			t.Fatal("timed out waiting for StoreEvent")
		}
	}
}

func waitSessionEvent(t *testing.T, observer *recordingObserver) SessionEvent {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case raw := <-observer.events:
			if typed, ok := raw.(SessionEvent); ok {
				return typed
			}
		case <-deadline:
			t.Fatal("timed out waiting for SessionEvent")
		}
	}
}

type panicObserver struct{}

func (panicObserver) ObserveTLRPC(Event) { panic("observer panic") }

type blockingObserver struct {
	started chan struct{}
	release chan struct{}
}

func (o *blockingObserver) ObserveTLRPC(Event) {
	close(o.started)
	<-o.release
}

func TestObserverInterceptorEmitsRPCClassification(t *testing.T) {
	observer := newRecordingObserver()
	server := NewServer(WithObserver(observer))
	interceptor := server.unaryInterceptors[0]
	ctx := withBinding(context.Background(), Binding{AuthKeyID: 77, SessionID: 88, Layer: 228})
	ctx = withAuthKeyID(ctx, 77)
	ctx = withLayer(ctx, 228)

	_, err := interceptor(ctx, &testServiceRequest{}, &UnaryServerInfo{FullMethod: "/svc/Call"}, func(context.Context, interface{}) (interface{}, error) {
		return nil, NewBadRequestError("BAD")
	})
	if err == nil {
		t.Fatal("interceptor returned nil error")
	}
	event := waitRPCEvent(t, observer)
	if event.Method != "/svc/Call" {
		t.Fatalf("method = %q, want /svc/Call", event.Method)
	}
	if event.ResultClass != "bad_request" {
		t.Fatalf("result class = %q, want bad_request", event.ResultClass)
	}
	if event.AuthKeyID != 77 || event.SessionID != 88 || event.Layer != 228 {
		t.Fatalf("event binding = %+v", event)
	}
}

func TestObservedSessionStoreEmitsClassification(t *testing.T) {
	observer := newRecordingObserver()
	key := session.SessionKey{AuthKeyID: 41, SessionID: 51}
	server := NewServer(WithObserver(observer))

	_, err := server.store.Load(context.Background(), key)
	if err == nil {
		t.Fatal("Load returned nil error, want not found")
	}
	event := waitStoreEvent(t, observer)
	if event.Operation != "load" {
		t.Fatalf("operation = %q, want load", event.Operation)
	}
	if event.Classification != "not_found" {
		t.Fatalf("classification = %q, want not_found", event.Classification)
	}
	if event.AuthKeyID != int64(key.AuthKeyID) || event.SessionID != key.SessionID {
		t.Fatalf("event key = %+v", event)
	}
}

func TestObserverPanicsDoNotEscapeServerOperations(t *testing.T) {
	server := NewServer(WithObserver(panicObserver{}))
	key := session.SessionKey{AuthKeyID: 1, SessionID: 2}
	if _, err := server.store.Load(context.Background(), key); err == nil {
		t.Fatal("Load returned nil error, want not found")
	}
	if err := server.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestObserverCannotBlockSinkClose(t *testing.T) {
	observer := &blockingObserver{started: make(chan struct{}), release: make(chan struct{})}
	sink := newObserverSink(observer)
	sink.emit(GaugeEvent{Name: "active_connections", Value: 1})
	select {
	case <-observer.started:
	case <-time.After(time.Second):
		t.Fatal("observer did not start")
	}
	done := make(chan struct{})
	go func() {
		sink.close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sink close waited for blocked observer")
	}
	close(observer.release)
}
