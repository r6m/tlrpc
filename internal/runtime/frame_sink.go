package runtime

import (
	"context"
	"sync"
)

// connectionFrameSink is the physical write boundary shared by every session
// writer attached to one connection. It serializes complete MTProto frames so
// transports such as WebSocket are never written concurrently.
//
// Close intentionally does not close the underlying FrameConnection. A writer
// owns only its session-local lifecycle; Connection shutdown is the sole owner
// of the physical transport lifecycle.
type connectionFrameSink struct {
	connection FrameConnection
	writeMu    sync.Mutex
}

func newConnectionFrameSink(connection FrameConnection) *connectionFrameSink {
	return &connectionFrameSink{connection: connection}
}

func (s *connectionFrameSink) WriteFrame(ctx context.Context, frame []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	return s.connection.WriteMessage(frame)
}

func (*connectionFrameSink) Close() error { return nil }

var _ FrameSink = (*connectionFrameSink)(nil)
