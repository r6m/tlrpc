//go:build gotd_integ

package tlrpc

import (
	"sync/atomic"
	"time"

	"github.com/r6m/tlrpc/session"
)

// GotdTestHooks controls gotd integration behavior in tests.
type GotdTestHooks struct {
	ForceBadServerSaltOnce atomic.Bool
	EncryptedRequestCount  atomic.Int32
	ForcedSalt             atomic.Int64
}

// WithGotdTestHooks registers gotd-only integration test hooks.
func WithGotdTestHooks(hooks *GotdTestHooks) ServerOption {
	return func(s *Server) {
		s.gotdTestHooks = hooks
	}
}

func (s *Server) maybeForceBadServerSalt(_ *session.Session, _ int64, _ int32) (int64, bool) {
	hooks, _ := s.gotdTestHooks.(*GotdTestHooks)
	if hooks == nil {
		return 0, false
	}
	hooks.EncryptedRequestCount.Add(1)
	if !hooks.ForceBadServerSaltOnce.CompareAndSwap(true, false) {
		return 0, false
	}
	newSalt := hooks.ForcedSalt.Load()
	if newSalt == 0 {
		newSalt = time.Now().UTC().UnixNano()
		hooks.ForcedSalt.Store(newSalt)
	}
	return newSalt, true
}
