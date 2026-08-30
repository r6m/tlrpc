package runtime

import (
	"context"
	"fmt"
	"sync"
)

// InvalidActiveRequestCapacityError reports an unusable registry capacity.
type InvalidActiveRequestCapacityError struct {
	Capacity int
}

func (e *InvalidActiveRequestCapacityError) Error() string {
	return fmt.Sprintf("runtime: active request capacity must be positive: %d", e.Capacity)
}

// InvalidActiveRequestIDError reports an invalid inbound request message ID.
type InvalidActiveRequestIDError struct {
	ID int64
}

func (e *InvalidActiveRequestIDError) Error() string {
	return fmt.Sprintf("runtime: active request message ID must be non-zero: %d", e.ID)
}

// DuplicateActiveRequestError reports an already-running request message ID.
type DuplicateActiveRequestError struct {
	ID int64
}

func (e *DuplicateActiveRequestError) Error() string {
	return fmt.Sprintf("runtime: active request already exists: %d", e.ID)
}

// ActiveRequestCapacityError reports that no more handlers may be started.
type ActiveRequestCapacityError struct {
	Capacity int
}

func (e *ActiveRequestCapacityError) Error() string {
	return fmt.Sprintf("runtime: active request capacity reached: %d", e.Capacity)
}

// DropStatus describes the result of an rpc_drop_answer lookup.
type DropStatus uint8

const (
	// DropStatusUnknown maps to rpc_answer_unknown.
	DropStatusUnknown DropStatus = iota
	// DropStatusDroppedRunning maps to rpc_answer_dropped_running.
	DropStatusDroppedRunning
)

type activeRequest struct {
	cancel context.CancelCauseFunc
}

// ActiveRequestRegistry owns the cancellation lifetimes of running inbound RPCs.
//
// Dropped and completed requests are removed. Consequently, a repeated drop is
// deterministically reported as DropStatusUnknown.
type ActiveRequestRegistry struct {
	mu       sync.Mutex
	capacity int
	active   map[int64]*activeRequest
}

// NewActiveRequestRegistry constructs a bounded active-request registry.
func NewActiveRequestRegistry(capacity int) (*ActiveRequestRegistry, error) {
	if capacity <= 0 {
		return nil, &InvalidActiveRequestCapacityError{Capacity: capacity}
	}

	return &ActiveRequestRegistry{
		capacity: capacity,
		active:   make(map[int64]*activeRequest, capacity),
	}, nil
}

// Begin registers an inbound request and returns its handler context and an
// idempotent completion function. The completion function removes only the
// exact registration created by this call, so it cannot remove a later request
// that reuses the same message ID.
func (r *ActiveRequestRegistry) Begin(parentCtx context.Context, id int64) (context.Context, func(), error) {
	if id == 0 {
		return nil, nil, &InvalidActiveRequestIDError{ID: id}
	}
	if parentCtx == nil {
		parentCtx = context.Background()
	}

	handlerCtx, cancel := context.WithCancelCause(parentCtx)
	request := &activeRequest{cancel: cancel}

	r.mu.Lock()
	if _, exists := r.active[id]; exists {
		r.mu.Unlock()
		cancel(context.Canceled)
		return nil, nil, &DuplicateActiveRequestError{ID: id}
	}
	if len(r.active) >= r.capacity {
		r.mu.Unlock()
		cancel(context.Canceled)
		return nil, nil, &ActiveRequestCapacityError{Capacity: r.capacity}
	}
	r.active[id] = request
	r.mu.Unlock()

	var once sync.Once
	complete := func() {
		once.Do(func() {
			r.mu.Lock()
			if r.active[id] == request {
				delete(r.active, id)
			}
			r.mu.Unlock()
			cancel(context.Canceled)
		})
	}

	return handlerCtx, complete, nil
}

// Drop cancels and removes a running request atomically.
func (r *ActiveRequestRegistry) Drop(id int64) DropStatus {
	r.mu.Lock()
	request, exists := r.active[id]
	if exists {
		delete(r.active, id)
	}
	r.mu.Unlock()

	if !exists {
		return DropStatusUnknown
	}

	request.cancel(context.Canceled)
	return DropStatusDroppedRunning
}

// CancelAll cancels every active handler with cause and drains the registry.
func (r *ActiveRequestRegistry) CancelAll(cause error) {
	if cause == nil {
		cause = context.Canceled
	}

	r.mu.Lock()
	requests := make([]*activeRequest, 0, len(r.active))
	for id, request := range r.active {
		requests = append(requests, request)
		delete(r.active, id)
	}
	r.mu.Unlock()

	for _, request := range requests {
		request.cancel(cause)
	}
}
