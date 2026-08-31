package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

const (
	DefaultPhysicalWriteQueueCapacity = 64
	DefaultPhysicalWriteTimeout       = 30 * time.Second
)

var ErrPhysicalWriteQueueFull = errors.New("runtime: physical write queue deadline exceeded")

type FrameSinkPolicy struct {
	QueueCapacity int
	WriteTimeout  time.Duration
	Observe       func(bytes int, outcome string, err error, duration time.Duration)
}

// connectionFrameSink is the physical write boundary shared by every session
// writer attached to one connection. It serializes complete MTProto frames so
// transports such as WebSocket are never written concurrently.
//
// Close intentionally does not close the underlying FrameConnection. A writer
// owns only its session-local lifecycle; Connection shutdown is the sole owner
// of the physical transport lifecycle.
type connectionFrameSink struct {
	connection   FrameConnection
	writePermit  chan struct{}
	queueSlots   chan struct{}
	writeTimeout time.Duration
	observe      func(bytes int, outcome string, err error, duration time.Duration)
}

func newConnectionFrameSink(connection FrameConnection, policies ...FrameSinkPolicy) *connectionFrameSink {
	policy := FrameSinkPolicy{
		QueueCapacity: DefaultPhysicalWriteQueueCapacity,
		WriteTimeout:  DefaultPhysicalWriteTimeout,
	}
	if len(policies) != 0 {
		if policies[0].QueueCapacity > 0 {
			policy.QueueCapacity = policies[0].QueueCapacity
		}
		if policies[0].WriteTimeout > 0 {
			policy.WriteTimeout = policies[0].WriteTimeout
		}
	}
	sink := &connectionFrameSink{
		connection:   connection,
		writePermit:  make(chan struct{}, 1),
		queueSlots:   make(chan struct{}, policy.QueueCapacity+1),
		writeTimeout: policy.WriteTimeout,
		observe:      policy.Observe,
	}
	sink.writePermit <- struct{}{}
	return sink
}

func (s *connectionFrameSink) WriteFrame(ctx context.Context, frame []byte) error {
	started := time.Now()
	finish := func(outcome string, err error) error {
		if s.observe != nil && err != nil {
			s.observe(len(frame), outcome, err, time.Since(started))
		}
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	writeCtx := ctx
	cancel := func() {}
	if s.writeTimeout > 0 {
		writeCtx, cancel = context.WithTimeout(ctx, s.writeTimeout)
	}
	defer cancel()

	select {
	case s.queueSlots <- struct{}{}:
		defer func() { <-s.queueSlots }()
	case <-writeCtx.Done():
		return finish("pressure", writeQueueError(writeCtx))
	}

	select {
	case <-s.writePermit:
		defer func() { s.writePermit <- struct{}{} }()
	case <-writeCtx.Done():
		return finish("pressure", writeQueueError(writeCtx))
	}

	if err := writeCtx.Err(); err != nil {
		return finish("failed", err)
	}
	clearDeadline, err := setPhysicalWriteDeadline(s.connection, writeCtx)
	if err != nil {
		return finish("failed", err)
	}
	defer func() { _ = clearDeadline() }()
	return finish("failed", s.connection.WriteMessage(frame))
}

func writeQueueError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", ErrPhysicalWriteQueueFull, os.ErrDeadlineExceeded)
	}
	return ctx.Err()
}

func setPhysicalWriteDeadline(connection FrameConnection, ctx context.Context) (func() error, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return func() error { return nil }, nil
	}
	if err := connection.SetWriteDeadline(deadline); err != nil {
		return nil, err
	}
	canceled := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = connection.SetWriteDeadline(time.Now())
		close(canceled)
	})
	return func() error {
		if !stop() {
			<-canceled
		}
		return connection.SetWriteDeadline(time.Time{})
	}, nil
}

func (*connectionFrameSink) Close() error { return nil }

var _ FrameSink = (*connectionFrameSink)(nil)
