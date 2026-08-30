package tlrpc

import (
	"context"
	"sync"
	"testing"
)

// mockTLObject implements TLObject interface for testing
type mockTLObject struct {
	id uint32
}

func (m *mockTLObject) ConstructorID() uint32 {
	return m.id
}

func TestNewDispatcher(t *testing.T) {
	d := newDispatcher()

	if d == nil {
		t.Fatal("newDispatcher returned nil")
	}

	if d.constructors == nil {
		t.Error("constructors map not initialized")
	}

	if d.methods == nil {
		t.Error("methods map not initialized")
	}
}

func TestDispatcherRegisterConstructor(t *testing.T) {
	d := newDispatcher()

	constructorID := uint32(0x12345678)
	constructor := func() TLObject {
		return &mockTLObject{id: constructorID}
	}

	d.RegisterConstructor(constructorID, constructor)

	// Verify constructor was registered
	retrieved, ok := d.LookupConstructor(constructorID)
	if !ok {
		t.Error("constructor not found after registration")
	}

	if retrieved == nil {
		t.Error("retrieved constructor is nil")
	}

	// Test that constructor works
	obj := retrieved()
	if obj == nil {
		t.Error("constructor returned nil")
	}
	if obj.ConstructorID() != constructorID {
		t.Errorf("constructor returned wrong ID: got %x, want %x", obj.ConstructorID(), constructorID)
	}
}

func TestDispatcherLookupConstructorNotFound(t *testing.T) {
	d := newDispatcher()

	_, ok := d.LookupConstructor(0x12345678)
	if ok {
		t.Error("LookupConstructor returned true for unregistered ID")
	}
}

func TestDispatcherRegisterMethod(t *testing.T) {
	d := newDispatcher()

	methodID := uint32(0x87654321)
	method := func(ctx context.Context, req TLObject) (interface{}, error) {
		return "test_response", nil
	}

	d.RegisterMethod(methodID, method)

	// Verify method was registered
	retrieved, ok := d.LookupMethod(methodID)
	if !ok {
		t.Error("method not found after registration")
	}

	if retrieved == nil {
		t.Error("retrieved method is nil")
	}

	// Test that method works
	req := &mockTLObject{id: 0x11111111}
	resp, err := retrieved(context.Background(), req)
	if err != nil {
		t.Errorf("method returned error: %v", err)
	}
	if resp != "test_response" {
		t.Errorf("method returned wrong response: got %v, want %v", resp, "test_response")
	}
}

func TestDispatcherLookupMethodNotFound(t *testing.T) {
	d := newDispatcher()

	_, ok := d.LookupMethod(0x87654321)
	if ok {
		t.Error("LookupMethod returned true for unregistered ID")
	}
}

func TestDispatcherConcurrency(t *testing.T) {
	d := newDispatcher()
	const numGoroutines = 10
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2) // One for constructors, one for methods

	// Test concurrent constructor registration
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			constructorID := uint32(0x10000000 + id)
			constructor := func() TLObject {
				return &mockTLObject{id: constructorID}
			}
			for j := 0; j < numOperations; j++ {
				d.RegisterConstructor(constructorID+uint32(j), constructor)
				d.LookupConstructor(constructorID + uint32(j))
			}
		}(i)
	}

	// Test concurrent method registration
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			methodID := uint32(0x20000000 + id)
			method := func(ctx context.Context, req TLObject) (interface{}, error) {
				return "concurrent_response", nil
			}
			for j := 0; j < numOperations; j++ {
				d.RegisterMethod(methodID+uint32(j), method)
				d.LookupMethod(methodID + uint32(j))
			}
		}(i)
	}

	wg.Wait()

	// Verify that some registrations worked (exact count may vary due to overwrites)
	constructorCount := 0
	methodCount := 0

	for i := uint32(0); i < numGoroutines*numOperations; i++ {
		if _, ok := d.LookupConstructor(0x10000000 + i); ok {
			constructorCount++
		}
		if _, ok := d.LookupMethod(0x20000000 + i); ok {
			methodCount++
		}
	}

	if constructorCount == 0 {
		t.Error("no constructors were registered in concurrent test")
	}
	if methodCount == 0 {
		t.Error("no methods were registered in concurrent test")
	}
}

func TestDispatcherOverwrite(t *testing.T) {
	d := newDispatcher()

	constructorID := uint32(0x12345678)

	// Register first constructor
	constructor1 := func() TLObject {
		return &mockTLObject{id: 0x11111111}
	}
	d.RegisterConstructor(constructorID, constructor1)

	// Register second constructor with same ID (should overwrite)
	constructor2 := func() TLObject {
		return &mockTLObject{id: 0x22222222}
	}
	d.RegisterConstructor(constructorID, constructor2)

	// Verify the second one is stored
	retrieved, ok := d.LookupConstructor(constructorID)
	if !ok {
		t.Error("constructor not found after overwrite")
	}

	obj := retrieved()
	if obj.ConstructorID() != 0x22222222 {
		t.Error("constructor was not overwritten")
	}
}
