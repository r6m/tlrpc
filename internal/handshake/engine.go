// Package handshake implements the server side of the unencrypted MTProto
// authorization handshake.
package handshake

import (
	"crypto/rand"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/r6m/tlrpc/crypto"
)

const (
	DefaultCapacity = 4096
	DefaultTTL      = 2 * time.Minute
)

var (
	ErrInvalidConfig      = errors.New("handshake: invalid engine configuration")
	ErrCapacity           = errors.New("handshake: session capacity reached")
	ErrExpired            = errors.New("handshake: session expired")
	ErrClosed             = errors.New("handshake: session closed")
	ErrInvalidHandshake   = errors.New("handshake: invalid message")
	ErrUnsupportedMessage = errors.New("handshake: unsupported message")
)

// Clock and Random are optional test seams. Production callers should leave
// them nil so the engine uses time.Now and crypto/rand.Reader.
type Config struct {
	AuthKeys   crypto.AuthKeyManager
	ServerKeys crypto.ServerKeyManager
	Capacity   int
	TTL        time.Duration
	Clock      func() time.Time
	Random     io.Reader
}

// Engine owns admission and lifetime for all in-progress handshakes belonging
// to one server. Protocol state itself is held by a connection-scoped Session.
type Engine struct {
	authKeys   crypto.AuthKeyManager
	serverKeys crypto.ServerKeyManager
	capacity   int
	ttl        time.Duration
	now        func() time.Time
	random     io.Reader

	mu       sync.Mutex
	sessions map[*Session]time.Time
	randomMu sync.Mutex
}

func New(config Config) (*Engine, error) {
	if config.AuthKeys == nil || config.ServerKeys == nil {
		return nil, ErrInvalidConfig
	}
	if config.Capacity < 0 || config.TTL < 0 {
		return nil, ErrInvalidConfig
	}
	if config.Capacity == 0 {
		config.Capacity = DefaultCapacity
	}
	if config.TTL == 0 {
		config.TTL = DefaultTTL
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	return &Engine{
		authKeys:   config.AuthKeys,
		serverKeys: config.ServerKeys,
		capacity:   config.Capacity,
		ttl:        config.TTL,
		now:        config.Clock,
		random:     config.Random,
		sessions:   make(map[*Session]time.Time),
	}, nil
}

// NewSession admits one connection-scoped handshake session.
func (e *Engine) NewSession() (*Session, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := e.now()
	e.removeExpiredLocked(now)
	if len(e.sessions) >= e.capacity {
		return nil, ErrCapacity
	}
	session := &Session{engine: e, stage: stageFresh}
	e.sessions[session] = now.Add(e.ttl)
	return session, nil
}

func (e *Engine) check(session *Session) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	expiresAt, ok := e.sessions[session]
	if !ok {
		return ErrExpired
	}
	now := e.now()
	if !now.Before(expiresAt) {
		delete(e.sessions, session)
		return ErrExpired
	}
	return nil
}

func (e *Engine) refresh(session *Session) {
	e.mu.Lock()
	if _, ok := e.sessions[session]; ok {
		e.sessions[session] = e.now().Add(e.ttl)
	}
	e.mu.Unlock()
}

func (e *Engine) remove(session *Session) {
	e.mu.Lock()
	delete(e.sessions, session)
	e.mu.Unlock()
}

func (e *Engine) removeExpiredLocked(now time.Time) {
	for session, expiresAt := range e.sessions {
		if !now.Before(expiresAt) {
			delete(e.sessions, session)
		}
	}
}

func (e *Engine) readRandom(dst []byte) error {
	e.randomMu.Lock()
	defer e.randomMu.Unlock()
	_, err := io.ReadFull(e.random, dst)
	return err
}
