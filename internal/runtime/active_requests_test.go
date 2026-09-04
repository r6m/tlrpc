package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newActiveRequestRegistryForTest(t *testing.T, capacity int) *ActiveRequestRegistry {
	t.Helper()

	registry, err := NewActiveRequestRegistry(capacity)
	if err != nil {
		t.Fatalf("NewActiveRequestRegistry() error = %v", err)
	}
	return registry
}

func TestActiveRequestRegistryWaitDependenciesTracksSuccessAndFailure(t *testing.T) {
	registry := newActiveRequestRegistryForTest(t, 4)
	registrations, err := registry.beginBatch(context.Background(), []int64{4, 8})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- registry.WaitDependencies(context.Background(), []int64{4, 8}, time.Second)
	}()
	registrations[0].Complete(true)
	select {
	case err := <-done:
		t.Fatalf("wait returned before every dependency completed: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	registrations[1].Complete(true)
	if err := <-done; err != nil {
		t.Fatalf("successful dependencies: %v", err)
	}

	failed, err := registry.beginBatch(context.Background(), []int64{12})
	if err != nil {
		t.Fatal(err)
	}
	failed[0].Complete(false)
	if err := registry.WaitDependencies(context.Background(), []int64{12}, time.Second); !errors.Is(err, ErrInvokeAfterFailed) {
		t.Fatalf("failed dependency error = %v", err)
	}
}

func TestActiveRequestRegistryWaitDependenciesBoundsUnknownReference(t *testing.T) {
	registry := newActiveRequestRegistryForTest(t, 1)
	started := time.Now()
	err := registry.WaitDependencies(context.Background(), []int64{4}, 10*time.Millisecond)
	if !errors.Is(err, ErrInvokeAfterTimeout) {
		t.Fatalf("unknown dependency error = %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("unknown dependency wait was not bounded")
	}
}

func TestActiveRequestRegistryDropCancelsRunningRequest(t *testing.T) {
	registry := newActiveRequestRegistryForTest(t, 1)
	handlerCtx, complete, err := registry.Begin(context.Background(), 4)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer complete()

	if status := registry.Drop(4); status != DropStatusDroppedRunning {
		t.Fatalf("Drop() = %v, want %v", status, DropStatusDroppedRunning)
	}
	if err := handlerCtx.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("handler context error = %v, want context.Canceled", err)
	}
}

func TestActiveRequestRegistryRejectsDuplicate(t *testing.T) {
	registry := newActiveRequestRegistryForTest(t, 2)
	_, complete, err := registry.Begin(context.Background(), 4)
	if err != nil {
		t.Fatalf("first Begin() error = %v", err)
	}
	defer complete()

	_, _, err = registry.Begin(context.Background(), 4)
	var duplicate *DuplicateActiveRequestError
	if !errors.As(err, &duplicate) {
		t.Fatalf("second Begin() error = %v, want *DuplicateActiveRequestError", err)
	}
}

func TestActiveRequestRegistryEnforcesCapacity(t *testing.T) {
	registry := newActiveRequestRegistryForTest(t, 1)
	_, complete, err := registry.Begin(context.Background(), 4)
	if err != nil {
		t.Fatalf("first Begin() error = %v", err)
	}
	defer complete()

	_, _, err = registry.Begin(context.Background(), 8)
	var capacity *ActiveRequestCapacityError
	if !errors.As(err, &capacity) {
		t.Fatalf("second Begin() error = %v, want *ActiveRequestCapacityError", err)
	}
}

func TestActiveRequestRegistryContainerAdmissionIsAtomic(t *testing.T) {
	registry := newActiveRequestRegistryForTest(t, 2)
	_, complete, err := registry.Begin(context.Background(), 4)
	if err != nil {
		t.Fatalf("occupy slot: %v", err)
	}
	defer complete()

	_, err = registry.beginBatch(context.Background(), []int64{8, 12})
	var capacity *ActiveRequestCapacityError
	if !errors.As(err, &capacity) {
		t.Fatalf("beginBatch() error = %v, want *ActiveRequestCapacityError", err)
	}
	if status := registry.Drop(8); status != DropStatusUnknown {
		t.Fatalf("first batch ID was partially registered: %v", status)
	}
	if status := registry.Drop(12); status != DropStatusUnknown {
		t.Fatalf("second batch ID was partially registered: %v", status)
	}
}

func TestActiveRequestRegistryCompletionIsIdempotentAndReleasesID(t *testing.T) {
	registry := newActiveRequestRegistryForTest(t, 1)
	handlerCtx, complete, err := registry.Begin(context.Background(), 4)
	if err != nil {
		t.Fatalf("first Begin() error = %v", err)
	}

	complete()
	complete()
	if err := handlerCtx.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("completed handler context error = %v, want context.Canceled", err)
	}

	_, replacementComplete, err := registry.Begin(context.Background(), 4)
	if err != nil {
		t.Fatalf("Begin() after completion error = %v", err)
	}
	replacementComplete()
}

func TestActiveRequestRegistryReportsOnlyLiveRequests(t *testing.T) {
	registry, err := NewActiveRequestRegistry(1)
	if err != nil {
		t.Fatal(err)
	}
	_, complete, err := registry.Begin(context.Background(), 4)
	if err != nil {
		t.Fatal(err)
	}
	if !registry.IsActive(4) {
		t.Fatal("running request was not reported active")
	}
	complete()
	if registry.IsActive(4) {
		t.Fatal("completed request remained active")
	}
}

func TestActiveRequestRegistryRepeatedDropIsUnknown(t *testing.T) {
	registry := newActiveRequestRegistryForTest(t, 1)
	_, complete, err := registry.Begin(context.Background(), 4)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer complete()

	if status := registry.Drop(4); status != DropStatusDroppedRunning {
		t.Fatalf("first Drop() = %v, want %v", status, DropStatusDroppedRunning)
	}
	if status := registry.Drop(4); status != DropStatusUnknown {
		t.Fatalf("second Drop() = %v, want %v", status, DropStatusUnknown)
	}
}

func TestActiveRequestRegistryCancelAllCancelsAndDrains(t *testing.T) {
	registry := newActiveRequestRegistryForTest(t, 2)
	firstCtx, firstComplete, err := registry.Begin(context.Background(), 4)
	if err != nil {
		t.Fatalf("first Begin() error = %v", err)
	}
	defer firstComplete()
	secondCtx, secondComplete, err := registry.Begin(context.Background(), 8)
	if err != nil {
		t.Fatalf("second Begin() error = %v", err)
	}
	defer secondComplete()

	cause := errors.New("connection stopped")
	registry.CancelAll(cause)

	if !errors.Is(context.Cause(firstCtx), cause) {
		t.Fatalf("first context cause = %v, want %v", context.Cause(firstCtx), cause)
	}
	if !errors.Is(context.Cause(secondCtx), cause) {
		t.Fatalf("second context cause = %v, want %v", context.Cause(secondCtx), cause)
	}
	if status := registry.Drop(4); status != DropStatusUnknown {
		t.Fatalf("Drop() after CancelAll = %v, want %v", status, DropStatusUnknown)
	}

	_, complete, err := registry.Begin(context.Background(), 12)
	if err != nil {
		t.Fatalf("Begin() after CancelAll error = %v", err)
	}
	complete()
}

func TestActiveRequestRegistryRejectsInvalidConfigurationAndID(t *testing.T) {
	_, err := NewActiveRequestRegistry(0)
	var invalidCapacity *InvalidActiveRequestCapacityError
	if !errors.As(err, &invalidCapacity) {
		t.Fatalf("NewActiveRequestRegistry() error = %v, want *InvalidActiveRequestCapacityError", err)
	}

	registry := newActiveRequestRegistryForTest(t, 1)
	_, _, err = registry.Begin(context.Background(), 0)
	var invalidID *InvalidActiveRequestIDError
	if !errors.As(err, &invalidID) {
		t.Fatalf("Begin() error = %v, want *InvalidActiveRequestIDError", err)
	}
}
