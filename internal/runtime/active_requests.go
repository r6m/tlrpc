package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrInvokeAfterFailed  = errors.New("runtime: invoke-after dependency failed")
	ErrInvokeAfterTimeout = errors.New("runtime: invoke-after dependency timed out")
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
	ended  bool
}

type activeRequestRegistration struct {
	Context  context.Context
	Complete func(bool)
}

// ActiveRequestRegistry owns the cancellation lifetimes of running inbound RPCs.
//
// Dropped and completed requests are removed. Consequently, a repeated drop is
// deterministically reported as DropStatusUnknown.
type ActiveRequestRegistry struct {
	mu             sync.Mutex
	capacity       int
	active         map[int64]*activeRequest
	completed      map[int64]bool
	completedOrder []int64
	changed        chan struct{}
}

// NewActiveRequestRegistry constructs a bounded active-request registry.
func NewActiveRequestRegistry(capacity int) (*ActiveRequestRegistry, error) {
	if capacity <= 0 {
		return nil, &InvalidActiveRequestCapacityError{Capacity: capacity}
	}

	return &ActiveRequestRegistry{
		capacity:  capacity,
		active:    make(map[int64]*activeRequest, capacity),
		completed: make(map[int64]bool, capacity),
		changed:   make(chan struct{}),
	}, nil
}

// Begin registers an inbound request and returns its handler context and an
// idempotent completion function. The completion function removes only the
// exact registration created by this call, so it cannot remove a later request
// that reuses the same message ID.
func (r *ActiveRequestRegistry) Begin(parentCtx context.Context, id int64) (context.Context, func(), error) {
	registrations, err := r.beginBatch(parentCtx, []int64{id})
	if err != nil {
		return nil, nil, err
	}
	return registrations[0].Context, func() { registrations[0].Complete(true) }, nil
}

// beginBatch atomically registers a complete container's content requests.
// Either every ID receives a handler lifetime or the registry remains unchanged.
func (r *ActiveRequestRegistry) beginBatch(parentCtx context.Context, ids []int64) ([]activeRequestRegistration, error) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			return nil, &InvalidActiveRequestIDError{ID: id}
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, &DuplicateActiveRequestError{ID: id}
		}
		seen[id] = struct{}{}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.active)+len(ids) > r.capacity {
		return nil, &ActiveRequestCapacityError{Capacity: r.capacity}
	}
	for _, id := range ids {
		if _, exists := r.active[id]; exists {
			return nil, &DuplicateActiveRequestError{ID: id}
		}
	}

	registrations := make([]activeRequestRegistration, 0, len(ids))
	for _, id := range ids {
		handlerCtx, cancel := context.WithCancelCause(parentCtx)
		request := &activeRequest{cancel: cancel}
		r.active[id] = request
		var once sync.Once
		complete := func(success bool) {
			once.Do(func() {
				r.mu.Lock()
				r.finishLocked(id, request, success)
				r.mu.Unlock()
				cancel(context.Canceled)
			})
		}
		registrations = append(registrations, activeRequestRegistration{Context: handlerCtx, Complete: complete})
	}
	r.notifyLocked()
	return registrations, nil
}

// WaitDependencies waits for all referenced requests to complete successfully.
// Completion history is bounded by the registry capacity. Unknown references
// wait for an earlier out-of-order request to arrive until timeout.
func (r *ActiveRequestRegistry) WaitDependencies(ctx context.Context, ids []int64, timeout time.Duration) error {
	if len(ids) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		return ErrInvokeAfterTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		allComplete := true
		r.mu.Lock()
		for _, id := range ids {
			if success, ok := r.completed[id]; ok {
				if !success {
					r.mu.Unlock()
					return ErrInvokeAfterFailed
				}
				continue
			}
			allComplete = false
		}
		changed := r.changed
		r.mu.Unlock()
		if allComplete {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return ErrInvokeAfterTimeout
		case <-changed:
		}
	}
}

// Drop cancels and removes a running request atomically.
func (r *ActiveRequestRegistry) Drop(id int64) DropStatus {
	r.mu.Lock()
	request, exists := r.active[id]
	if exists {
		r.finishLocked(id, request, false)
	}
	r.mu.Unlock()

	if !exists {
		return DropStatusUnknown
	}

	request.cancel(context.Canceled)
	return DropStatusDroppedRunning
}

// IsActive reports whether id currently owns a handler lifetime. It is used by
// replay recovery to leave a live request alone so its original rpc_result can
// settle the client request.
func (r *ActiveRequestRegistry) IsActive(id int64) bool {
	if r == nil || id == 0 {
		return false
	}
	r.mu.Lock()
	_, active := r.active[id]
	r.mu.Unlock()
	return active
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
		r.finishLocked(id, request, false)
	}
	r.mu.Unlock()

	for _, request := range requests {
		request.cancel(cause)
	}
}

func (r *ActiveRequestRegistry) finishLocked(id int64, request *activeRequest, success bool) {
	if request == nil || request.ended {
		return
	}
	request.ended = true
	if r.active[id] == request {
		delete(r.active, id)
	}
	if _, exists := r.completed[id]; exists {
		for index, completedID := range r.completedOrder {
			if completedID == id {
				copy(r.completedOrder[index:], r.completedOrder[index+1:])
				r.completedOrder = r.completedOrder[:len(r.completedOrder)-1]
				break
			}
		}
	}
	r.completed[id] = success
	r.completedOrder = append(r.completedOrder, id)
	if len(r.completedOrder) > r.capacity {
		oldest := r.completedOrder[0]
		r.completedOrder = r.completedOrder[1:]
		delete(r.completed, oldest)
	}
	r.notifyLocked()
}

func (r *ActiveRequestRegistry) notifyLocked() {
	close(r.changed)
	r.changed = make(chan struct{})
}
