package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var ErrRecoveryPushBarrierFull = errors.New("runtime: recovery push barrier queue full")

var errRecoveryPushBarrierAborted = errors.New("runtime: recovery push barrier request ended without a written reply")

type recoveryPushSubmit func(context.Context, Intent) error

// recoveryPushBarrier is owned by one connectionSession lease generation.
// It gates only Push intents; protected replies use the normal outcome path.
type recoveryPushBarrier struct {
	mu      sync.Mutex
	orderMu sync.Mutex

	ctx      context.Context
	maxCount int
	maxBytes int
	submit   recoveryPushSubmit
	retire   func(error)

	active      int
	draining    bool
	queuedBytes int
	queue       [][]byte
	closed      bool
	cause       error
}

type recoveryPushBarrierToken struct {
	barrier *recoveryPushBarrier
	once    sync.Once
}

func newRecoveryPushBarrier(ctx context.Context, maxCount, maxBytes int, submit recoveryPushSubmit, retire func(error)) *recoveryPushBarrier {
	if ctx == nil {
		ctx = context.Background()
	}
	return &recoveryPushBarrier{ctx: ctx, maxCount: maxCount, maxBytes: maxBytes, submit: submit, retire: retire}
}

func (b *recoveryPushBarrier) begin() *recoveryPushBarrierToken {
	b.orderMu.Lock()
	b.mu.Lock()
	if !b.closed {
		b.active++
	}
	b.mu.Unlock()
	b.orderMu.Unlock()
	return &recoveryPushBarrierToken{barrier: b}
}

func (b *recoveryPushBarrier) push(ctx context.Context, body []byte) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	b.mu.Lock()
	if b.closed {
		err := b.closeCauseLocked()
		b.mu.Unlock()
		return err
	}
	if b.active != 0 || b.draining {
		err := b.enqueueLocked(body)
		b.mu.Unlock()
		b.retireOverflow(err)
		return err
	}
	b.mu.Unlock()

	b.orderMu.Lock()
	b.mu.Lock()
	if b.closed {
		err := b.closeCauseLocked()
		b.mu.Unlock()
		b.orderMu.Unlock()
		return err
	}
	if b.active != 0 || b.draining {
		err := b.enqueueLocked(body)
		b.mu.Unlock()
		b.orderMu.Unlock()
		b.retireOverflow(err)
		return err
	}
	b.mu.Unlock()
	copyBody := append([]byte(nil), body...)
	err := b.submit(ctx, Push{Body: copyBody})
	b.orderMu.Unlock()
	return err
}

func (b *recoveryPushBarrier) enqueueLocked(body []byte) error {
	if b.closed {
		return b.closeCauseLocked()
	}
	if len(b.queue) >= b.maxCount || len(body) > b.maxBytes-b.queuedBytes {
		err := fmt.Errorf("%w: count %d/%d bytes %d+%d/%d", ErrRecoveryPushBarrierFull, len(b.queue), b.maxCount, b.queuedBytes, len(body), b.maxBytes)
		b.closeLocked(err)
		return err
	}
	copyBody := append([]byte(nil), body...)
	b.queue = append(b.queue, copyBody)
	b.queuedBytes += len(copyBody)
	return nil
}

func (b *recoveryPushBarrier) retireOverflow(err error) {
	if errors.Is(err, ErrRecoveryPushBarrierFull) && b.retire != nil {
		b.retire(err)
	}
}

func (t *recoveryPushBarrierToken) finish(submitReply func() error) error {
	if t == nil || t.barrier == nil {
		return submitReply()
	}
	var result error
	t.once.Do(func() {
		result = t.barrier.finish(submitReply)
	})
	return result
}

func (t *recoveryPushBarrierToken) cancel() {
	if t == nil || t.barrier == nil {
		return
	}
	t.once.Do(func() {
		t.barrier.abort(errRecoveryPushBarrierAborted)
	})
}

func (b *recoveryPushBarrier) finish(submitReply func() error) error {
	b.orderMu.Lock()
	b.mu.Lock()
	if b.closed {
		err := b.closeCauseLocked()
		b.mu.Unlock()
		b.orderMu.Unlock()
		return err
	}
	b.mu.Unlock()
	replyErr := submitReply()
	b.mu.Lock()
	if replyErr != nil {
		b.closeLocked(replyErr)
		retire := b.retire
		b.mu.Unlock()
		b.orderMu.Unlock()
		if retire != nil {
			retire(replyErr)
		}
		return replyErr
	}
	if b.closed {
		err := b.closeCauseLocked()
		b.mu.Unlock()
		b.orderMu.Unlock()
		return err
	}
	b.releaseLocked()
	startDrain := b.active == 0 && len(b.queue) != 0
	if startDrain {
		b.draining = true
	}
	b.mu.Unlock()
	if startDrain {
		drainErr := b.drainLocked()
		b.orderMu.Unlock()
		if drainErr != nil && b.retire != nil {
			b.retire(drainErr)
		}
		return drainErr
	}
	b.orderMu.Unlock()
	return nil
}

func (b *recoveryPushBarrier) abort(cause error) {
	b.orderMu.Lock()
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		b.orderMu.Unlock()
		return
	}
	b.closeLocked(cause)
	retire := b.retire
	b.mu.Unlock()
	b.orderMu.Unlock()
	if retire != nil {
		retire(cause)
	}
}

func (b *recoveryPushBarrier) releaseLocked() {
	if b.active > 0 {
		b.active--
	}
}

// drainLocked runs with orderMu held. Push may still append under mu and return
// accepted, while begin and every physical submit remain serialized behind it.
func (b *recoveryPushBarrier) drainLocked() error {
	for {
		b.mu.Lock()
		if b.closed {
			err := b.closeCauseLocked()
			b.draining = false
			b.mu.Unlock()
			return err
		}
		if b.active != 0 || len(b.queue) == 0 {
			b.draining = false
			b.mu.Unlock()
			return nil
		}
		body := b.queue[0]
		b.queue[0] = nil
		b.queue = b.queue[1:]
		b.queuedBytes -= len(body)
		b.mu.Unlock()

		if err := b.submit(b.ctx, Push{Body: body}); err != nil {
			b.mu.Lock()
			b.closeLocked(err)
			b.mu.Unlock()
			return err
		}
	}
}

func (b *recoveryPushBarrier) close(cause error) {
	b.orderMu.Lock()
	b.mu.Lock()
	b.closeLocked(cause)
	b.mu.Unlock()
	b.orderMu.Unlock()
}

func (b *recoveryPushBarrier) closeLocked(cause error) {
	if b.closed {
		return
	}
	if cause == nil {
		cause = context.Canceled
	}
	b.closed = true
	b.cause = cause
	b.queue = nil
	b.queuedBytes = 0
}

func (b *recoveryPushBarrier) closeCauseLocked() error {
	if b.cause != nil {
		return b.cause
	}
	return context.Canceled
}
