package runtime

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/internal/handshake"
	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/mtproto/protocol"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
)

const (
	DefaultActiveRequestCapacity     = 1024
	DefaultConnectionSessionCapacity = 16
	transportErrorAuthKeyNotFound    = int32(-404)
)

var (
	ErrConnectionConfig         = errors.New("runtime: incomplete connection configuration")
	ErrConnectionProtocol       = errors.New("runtime: invalid connection protocol transition")
	ErrHandshakeAuthKeyMismatch = errors.New("runtime: encrypted auth key differs from completed handshake")
	ErrUnknownSessionProgress   = errors.New("runtime: unknown session starts after its initial sequence")
)

// ConnectionSessionCapacityError reports that a physical transport already
// owns the maximum number of independently leased MTProto sessions.
type ConnectionSessionCapacityError struct {
	Capacity int
}

func (e *ConnectionSessionCapacityError) Error() string {
	return fmt.Sprintf("runtime: connection session capacity reached: %d", e.Capacity)
}

// FrameConnection is the transport-neutral message boundary consumed by
// Runtime v2. TCP and WebSocket adapters both satisfy this contract.
type FrameConnection interface {
	ReadMessage(maxPayloadBytes int) ([]byte, error)
	WriteMessage(frame []byte) error
	SetWriteDeadline(time.Time) error
	Close() error
	Context() context.Context
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
}

type ConnectionConfig struct {
	ConnectionID               uint64
	Conn                       FrameConnection
	AuthKeys                   AuthKeySource
	Handshake                  *handshake.Engine
	Sessions                   session.Coordinator
	Reliability                *ReliabilityRegistry
	Application                ApplicationDispatcher
	MessageIDs                 MessageIDSource
	MaxPayloadBytes            int
	MaxDecodedPayload          int
	DecodeLimits               mtproto.DecodeLimits
	MaxEncodedBytes            int
	FrameSinkPolicy            FrameSinkPolicy
	ActiveRequests             int
	SessionCapacity            int
	Transport                  string
	SchemaLayer                int
	Now                        func() time.Time
	Presence                   SessionPresence
	RecoveryPushBarrierMethods map[uint32]struct{}
	RecoveryPushBarrierCount   int
	RecoveryPushBarrierBytes   int
	NonSubscribingMethods      map[uint32]struct{}
}

// SessionPresence receives semantic sender availability for active composite
// sessions. Implementations must use sender identity when removing a binding.
type SessionPresence interface {
	Update(snapshot session.Snapshot, leaseGeneration int64, sender Sender, acceptsPush bool)
	Remove(key session.SessionKey, sender Sender)
}

// Connection composes the Runtime v2 stages for one accepted transport peer.
// No protocol state or writer is shared directly with another connection.
type Connection struct {
	config     ConnectionConfig
	messageIDs MessageIDSource
	now        func() time.Time
	frameSink  *connectionFrameSink

	mu            sync.Mutex
	sessions      map[session.SessionKey]*connectionSession
	poisoned      map[session.SessionKey]struct{}
	authKeyID     crypto.KeyID
	authKey       crypto.AuthKey
	authKeyPinned bool
	authKeyCached bool
	admission     *connectionRequestAdmission

	disconnectMu         sync.Mutex
	disconnectTimer      *time.Timer
	disconnectGeneration uint64
	disconnectClosed     bool

	handshakeSession *handshake.Session
	authorization    *handshake.Result
}

func NewConnection(config ConnectionConfig) (*Connection, error) {
	if config.Conn == nil || config.AuthKeys == nil || config.Handshake == nil ||
		config.Sessions == nil || config.Reliability == nil || config.Application == nil {
		return nil, ErrConnectionConfig
	}
	if config.ActiveRequests == 0 {
		config.ActiveRequests = DefaultActiveRequestCapacity
	}
	if config.ActiveRequests < 0 {
		return nil, ErrConnectionConfig
	}
	if config.SessionCapacity == 0 {
		config.SessionCapacity = DefaultConnectionSessionCapacity
	}
	if config.SessionCapacity < 0 {
		return nil, ErrConnectionConfig
	}
	if _, err := mtproto.NewDecodeBudget(config.DecodeLimits); err != nil {
		return nil, ErrConnectionConfig
	}
	if config.MaxEncodedBytes < 0 || config.FrameSinkPolicy.QueueCapacity < 0 || config.FrameSinkPolicy.WriteTimeout < 0 ||
		config.RecoveryPushBarrierCount < 0 || config.RecoveryPushBarrierBytes < 0 {
		return nil, ErrConnectionConfig
	}
	if len(config.RecoveryPushBarrierMethods) != 0 &&
		(config.RecoveryPushBarrierCount == 0 || config.RecoveryPushBarrierBytes == 0) {
		return nil, ErrConnectionConfig
	}
	messageIDs := config.MessageIDs
	if messageIDs == nil {
		messageIDs = mtproto.NewMsgIDGenerator()
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Connection{
		config:     config,
		messageIDs: &lockedMessageIDSource{source: messageIDs},
		now:        now,
		frameSink:  newConnectionFrameSink(config.Conn, config.FrameSinkPolicy),
		sessions:   make(map[session.SessionKey]*connectionSession, config.SessionCapacity),
		poisoned:   make(map[session.SessionKey]struct{}),
		admission:  newConnectionRequestAdmission(config.ActiveRequests),
	}, nil
}

// lockedMessageIDSource preserves the existing MessageIDSource contract while
// allowing independent session writers to allocate through it concurrently.
type lockedMessageIDSource struct {
	mu     sync.Mutex
	source MessageIDSource
}

func (s *lockedMessageIDSource) Next() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.source.Next()
}

func (c *Connection) Run(ctx context.Context) (runErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	defer func() { c.shutdown(runErr) }()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		frame, err := c.config.Conn.ReadMessage(c.config.MaxPayloadBytes)
		if err != nil {
			return err
		}
		decoded, err := DecodeFrame(frame, connectionAuthKeySource{connection: c})
		if err != nil {
			if errors.Is(err, crypto.ErrAuthKeyNotFound) {
				if writeErr := c.writeTransportError(ctx, transportErrorAuthKeyNotFound); writeErr != nil {
					return errors.Join(err, fmt.Errorf("runtime: write transport error %d: %w", transportErrorAuthKeyNotFound, writeErr))
				}
			}
			return err
		}
		if decoded.Unencrypted != nil {
			if err := c.handleUnencrypted(ctx, decoded.Unencrypted); err != nil {
				return err
			}
			continue
		}
		if err := c.handleEncrypted(ctx, decoded); err != nil {
			return err
		}
	}
}

func (c *Connection) writeTransportError(ctx context.Context, code int32) error {
	var payload [4]byte
	binary.LittleEndian.PutUint32(payload[:], uint32(code))
	return c.frameSink.WriteFrame(ctx, payload[:])
}

func (c *Connection) handleUnencrypted(ctx context.Context, message *mtproto.UnencryptedMessage) error {
	c.mu.Lock()
	bound := len(c.sessions) != 0 || c.authKeyPinned
	c.mu.Unlock()
	if bound {
		return fmt.Errorf("%w: plaintext_after_encrypted", ErrConnectionProtocol)
	}
	if handled, err := c.handleHandshakeAcknowledgement(message.Data); handled {
		return err
	}
	if c.authorization != nil {
		// PFS clients may generate the permanent and temporary keys serially
		// on one plaintext connection before sending their first encrypted RPC.
		if len(message.Data) != 20 {
			return fmt.Errorf("%w: unexpected_plaintext_after_handshake", ErrConnectionProtocol)
		}
		constructor := binary.LittleEndian.Uint32(message.Data)
		if constructor != 0xbe7e8ef1 && constructor != 0x60469778 {
			return fmt.Errorf("%w: unexpected_plaintext_after_handshake", ErrConnectionProtocol)
		}
		c.handshakeSession.Close()
		c.handshakeSession = nil
		c.authorization = nil
	}
	if c.handshakeSession == nil {
		session, err := c.config.Handshake.NewSession()
		if err != nil {
			return err
		}
		c.handshakeSession = session
	}
	output, err := c.handshakeSession.Handle(ctx, message.MsgID, message.Data)
	if err != nil {
		return err
	}
	response := &mtproto.UnencryptedMessage{
		MsgID: c.nextResponseMessageID(),
		Data:  output.Response,
	}
	frame, err := response.Serialize()
	if err != nil {
		return err
	}
	if err := c.config.Conn.WriteMessage(frame); err != nil {
		return err
	}
	if output.Result != nil {
		result := *output.Result
		c.authorization = &result
	}
	return nil
}

func (c *Connection) handleEncrypted(ctx context.Context, decoded DecodedFrame) error {
	inner := decoded.Encrypted
	if inner == nil {
		return fmt.Errorf("%w: missing_encrypted_inner_data", ErrConnectionProtocol)
	}
	if c.authorization != nil && c.authorization.AuthKeyID != decoded.AuthKeyID {
		return ErrHandshakeAuthKeyMismatch
	}
	actor, err := c.sessionFor(ctx, decoded)
	if err != nil {
		return err
	}
	return actor.handleEncrypted(ctx, inner)
}

func (c *Connection) sessionFor(ctx context.Context, decoded DecodedFrame) (*connectionSession, error) {
	inner := decoded.Encrypted
	if inner == nil {
		return nil, fmt.Errorf("%w: missing_encrypted_inner_data", ErrConnectionProtocol)
	}
	key := session.SessionKey{AuthKeyID: decoded.AuthKeyID, SessionID: inner.SessionID}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.authKeyPinned && c.authKeyID != decoded.AuthKeyID {
		return nil, fmt.Errorf("%w: different_auth_key", ErrConnectionProtocol)
	}
	if _, lost := c.poisoned[key]; lost {
		return nil, session.ErrLeaseLost
	}
	if actor := c.sessions[key]; actor != nil {
		return actor, nil
	}
	if len(c.sessions) >= c.config.SessionCapacity {
		return nil, &ConnectionSessionCapacityError{Capacity: c.config.SessionCapacity}
	}
	actor, err := newConnectionSession(ctx, c, decoded)
	if err != nil {
		return nil, err
	}
	c.authKeyID = decoded.AuthKeyID
	c.authKeyPinned = true
	if policy, ok := c.config.AuthKeys.(connectionAuthKeyCachePolicy); ok && policy.CacheAuthKeyForConnection() {
		cache := true
		if policy, ok := c.config.AuthKeys.(interface{ CacheAuthKeyIDForConnection(crypto.KeyID) bool }); ok {
			cache = policy.CacheAuthKeyIDForConnection(decoded.AuthKeyID)
		}
		if cache {
			c.authKey = decoded.AuthKey
			c.authKeyCached = true
		}
	}
	c.sessions[key] = actor
	actor.start()
	return actor, nil
}

// connectionAuthKeySource resolves the first encrypted frame from the
// configured source. Once a session lease has been acquired successfully, the
// physical connection serves that validated key locally and rejects key
// switches without another store lookup.
type connectionAuthKeySource struct{ connection *Connection }

// connectionAuthKeyCachePolicy is an explicit manager capability. Opting in
// asserts that authorization-key revocation also retires or rejects the
// associated active session lease; Runtime v2 cannot otherwise observe Delete
// while serving a connection-cached key.
type connectionAuthKeyCachePolicy interface {
	CacheAuthKeyForConnection() bool
}

func (s connectionAuthKeySource) Get(keyID crypto.KeyID) (crypto.AuthKey, error) {
	if s.connection == nil {
		return crypto.AuthKey{}, ErrConnectionConfig
	}
	s.connection.mu.Lock()
	pinned := s.connection.authKeyPinned
	pinnedID := s.connection.authKeyID
	pinnedKey := s.connection.authKey
	cached := s.connection.authKeyCached
	source := s.connection.config.AuthKeys
	s.connection.mu.Unlock()
	if !pinned {
		return source.Get(keyID)
	}
	if pinnedID != keyID {
		return crypto.AuthKey{}, fmt.Errorf("%w: different_auth_key", ErrConnectionProtocol)
	}
	if cached {
		return pinnedKey, nil
	}
	return source.Get(keyID)
}

func (c *Connection) removeSession(key session.SessionKey, actor *connectionSession, cause error) {
	c.mu.Lock()
	closeConnection := false
	if c.sessions[key] == actor {
		delete(c.sessions, key)
		if errors.Is(cause, session.ErrLeaseLost) {
			// A physical MTProto transport may multiplex independent sessions. Keep
			// those sessions alive, but never let this stale transport reclaim a
			// fenced session generation after another connection took ownership.
			c.poisoned[key] = struct{}{}
			// If this was the transport's last session, there is nothing left that
			// can make progress on the stale socket. Close it so the displaced client
			// observes EOF and reconnects instead of waiting forever for canceled RPCs.
			// Bound stale-key memory on a hostile, long-lived multiplexed socket.
			// Reaching the configured active-session capacity is enough evidence to
			// retire the physical transport instead of retaining more tombstones.
			closeConnection = len(c.sessions) == 0 || len(c.poisoned) >= c.config.SessionCapacity
		}
	}
	c.mu.Unlock()
	if closeConnection {
		_ = c.config.Conn.Close()
	}
}

func (c *Connection) requestInfo(snapshot session.Snapshot, leaseGeneration int64) RequestInfo {
	transportMode := c.config.Transport
	if provider, ok := c.config.Conn.(interface{ TransportMode() string }); ok {
		if negotiated := provider.TransportMode(); negotiated != "" {
			transportMode = negotiated
		}
	}
	info := RequestInfo{
		ConnectionID: c.config.ConnectionID,
		AuthKeyID:    snapshot.AuthKeyID, SessionID: snapshot.SessionID,
		LeaseGeneration: leaseGeneration,
		ServerSalt:      snapshot.ServerSalt, UserID: snapshot.UserID, Layer: snapshot.Layer,
		Client: snapshot.Client,
		Peer:   PeerInfo{Transport: transportMode},
	}
	if address := c.config.Conn.LocalAddr(); address != nil {
		info.Peer.LocalAddr = address.String()
	}
	if address := c.config.Conn.RemoteAddr(); address != nil {
		info.Peer.RemoteAddr = address.String()
	}
	return info
}

// resetDisconnectDelay schedules the physical transport to close after a
// ping_delay_disconnect interval. Reset and expiry are linearized under
// disconnectMu: whichever wins first prevents the other from changing the
// connection's terminal state.
func (c *Connection) resetDisconnectDelay(delay time.Duration) {
	if c == nil || c.config.Conn == nil {
		return
	}
	c.disconnectMu.Lock()
	if c.disconnectClosed {
		c.disconnectMu.Unlock()
		return
	}
	c.disconnectGeneration++
	generation := c.disconnectGeneration
	if c.disconnectTimer != nil {
		c.disconnectTimer.Stop()
	}
	c.disconnectTimer = time.AfterFunc(delay, func() { c.expireDisconnectDelay(generation) })
	c.disconnectMu.Unlock()
}

func (c *Connection) expireDisconnectDelay(generation uint64) {
	if c == nil || c.config.Conn == nil {
		return
	}
	c.disconnectMu.Lock()
	if c.disconnectClosed || c.disconnectGeneration != generation {
		c.disconnectMu.Unlock()
		return
	}
	// Mark the terminal state before releasing the lock. A racing reset must
	// observe it and cannot refresh a connection whose close is already due.
	c.disconnectClosed = true
	c.disconnectTimer = nil
	c.disconnectMu.Unlock()
	_ = c.config.Conn.Close()
}

func (c *Connection) stopDisconnectDelay() {
	if c == nil {
		return
	}
	c.disconnectMu.Lock()
	c.disconnectClosed = true
	c.disconnectGeneration++
	if c.disconnectTimer != nil {
		c.disconnectTimer.Stop()
		c.disconnectTimer = nil
	}
	c.disconnectMu.Unlock()
}

func (c *Connection) nextResponseMessageID() int64 {
	return c.messageIDs.Next()&^int64(3) | 1
}

func (c *Connection) shutdown(cause error) {
	c.stopDisconnectDelay()
	c.mu.Lock()
	actors := make([]*connectionSession, 0, len(c.sessions))
	for _, actor := range c.sessions {
		actors = append(actors, actor)
	}
	handshakeSession := c.handshakeSession
	c.mu.Unlock()
	for _, actor := range actors {
		actor.shutdown(cause)
	}
	if handshakeSession != nil {
		handshakeSession.Close()
	}
	_ = c.config.Conn.Close()
}

func badMessageIntent(bad *protocol.BadMessageError) (ProtocolReply, error) {
	var value interface{ SerializeTL(io.Writer) error }
	if bad.Code == protocol.CodeBadServerSalt {
		value = &mtprototl.BadServerSalt{
			BadMsgID: bad.MessageID, BadMsgSeq: bad.SequenceNo,
			ErrorCode: bad.Code, NewSalt: bad.ExpectedServerSalt,
		}
	} else {
		value = &mtprototl.BadMsgNotification{
			BadMsgID: bad.MessageID, BadMsgSeq: bad.SequenceNo, ErrorCode: bad.Code,
		}
	}
	body, err := serializeRuntimeTL(value)
	return ProtocolReply{Body: body}, err
}

type requestSender struct {
	writer       *Writer
	barrier      *recoveryPushBarrier
	suppress     bool
	connectionID uint64
}

func (s *requestSender) ConnectionID() uint64 {
	if s == nil {
		return 0
	}
	return s.connectionID
}

func (s *requestSender) Push(ctx context.Context, body []byte) error {
	if s == nil || s.writer == nil {
		return ErrConnectionConfig
	}
	if s.suppress {
		return nil
	}
	if s.barrier != nil {
		return s.barrier.push(ctx, body)
	}
	return s.writer.Submit(ctx, Push{Body: append([]byte(nil), body...)})
}

var _ Sender = (*requestSender)(nil)
