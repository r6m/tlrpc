package runtime

import (
	"context"
	"errors"
	"testing"
)

func newActiveRequestRegistryForTest(t *testing.T, capacity int) *ActiveRequestRegistry {
	t.Helper()

	registry, err := NewActiveRequestRegistry(capacity)
	if err != nil {
		t.Fatalf("NewActiveRequestRegistry() error = %v", err)
	}
	return registry
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
