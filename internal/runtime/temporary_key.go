package runtime

import (
	"context"
	"sync"
	"time"

	"github.com/r6m/tlrpc/crypto"
)

// Temporary connections retire at expiry even when the peer goes idle. Every
// outgoing frame also checks binding revocation and any shortened lifetime.
type temporaryFrameSink struct {
	owner  *Connection
	keys   crypto.TemporaryAuthKeyManager
	id     crypto.KeyID
	mu     sync.Mutex
	timer  *time.Timer
	closed bool
}

func (s *temporaryFrameSink) Close() error { return s.owner.frameSink.Close() }

func temporaryKeySink(ctx context.Context, owner *Connection, id crypto.KeyID) (FrameSink, error) {
	keys, ok := owner.config.AuthKeys.(crypto.TemporaryAuthKeyManager)
	if !ok {
		return owner.frameSink, nil
	}
	info, temporary, err := keys.TemporaryInfo(id)
	if err != nil {
		return nil, err
	}
	if !temporary {
		return owner.frameSink, nil
	}
	sink := &temporaryFrameSink{owner: owner, keys: keys, id: id}
	sink.timer = time.AfterFunc(time.Until(info.ExpiresAt), func() { _ = owner.config.Conn.Close() })
	context.AfterFunc(ctx, func() {
		sink.mu.Lock()
		defer sink.mu.Unlock()
		sink.closed = true
		sink.timer.Stop()
	})
	return sink, nil
}

func (s *temporaryFrameSink) WriteFrame(ctx context.Context, frame []byte) error {
	info, temporary, err := s.keys.TemporaryInfo(s.id)
	if err != nil {
		return err
	}
	if !temporary || !time.Now().Before(info.ExpiresAt) {
		return crypto.ErrAuthKeyNotFound
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return context.Canceled
	}
	s.timer.Reset(time.Until(info.ExpiresAt))
	s.mu.Unlock()
	return s.owner.frameSink.WriteFrame(ctx, frame)
}
