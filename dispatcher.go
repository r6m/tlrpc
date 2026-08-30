package tlrpc

import (
	"context"
	"sync"
)

// dispatcher handles internal registration and dispatch of TL objects and methods
type dispatcher struct {
	mu           sync.RWMutex
	constructors map[uint32]func() TLObject
	methods      map[uint32]func(context.Context, TLObject) (interface{}, error)
}

// newDispatcher creates a new dispatcher
func newDispatcher() *dispatcher {
	return &dispatcher{
		constructors: make(map[uint32]func() TLObject),
		methods:      make(map[uint32]func(context.Context, TLObject) (interface{}, error)),
	}
}

// RegisterConstructor registers a constructor function for a TL object type
func (d *dispatcher) RegisterConstructor(id uint32, constructor func() TLObject) {
	d.mu.Lock()
	d.constructors[id] = constructor
	d.mu.Unlock()
}

// RegisterMethod registers a method handler for RPC calls
func (d *dispatcher) RegisterMethod(id uint32, handler func(context.Context, TLObject) (interface{}, error)) {
	d.mu.Lock()
	d.methods[id] = handler
	d.mu.Unlock()
}

// LookupConstructor returns a constructor function for the given ID
func (d *dispatcher) LookupConstructor(id uint32) (func() TLObject, bool) {
	d.mu.RLock()
	constructor, ok := d.constructors[id]
	d.mu.RUnlock()
	return constructor, ok
}

// LookupMethod returns a method handler for the given ID
func (d *dispatcher) LookupMethod(id uint32) (func(context.Context, TLObject) (interface{}, error), bool) {
	d.mu.RLock()
	handler, ok := d.methods[id]
	d.mu.RUnlock()
	return handler, ok
}
