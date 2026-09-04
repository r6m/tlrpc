package runtime

import (
	"context"
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
	ConnectionID      uint64
	Conn              FrameConnection
	AuthKeys          AuthKeySource
	Handshake         *handshake.Engine
	Sessions          session.Coordinator
	Reliability       *ReliabilityRegistry
	Application       ApplicationDispatcher
	MessageIDs        MessageIDSource
	MaxPayloadBytes   int
	MaxDecodedPayload int
	DecodeLimits      mtproto.DecodeLimits
	MaxEncodedBytes   int
	FrameSinkPolicy   FrameSinkPolicy
	ActiveRequests    int
	SessionCapacity   int
	Transport         string
	SchemaLayer       int
	Now               func() time.Time
	Presence          SessionPresence
}

// SessionPresence receives semantic sender availability for active composite
// sessions. Implementations must use sender identity when removing a binding.
type SessionPresence interface {
	Update(snapshot session.Snapshot, sender Sender, acceptsPush bool)
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
	authKeyID     crypto.KeyID
	authKeyPinned bool
	admission     *connectionRequestAdmission

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
	if config.MaxEncodedBytes < 0 || config.FrameSinkPolicy.QueueCapacity < 0 || config.FrameSinkPolicy.WriteTimeout < 0 {
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
		decoded, err := DecodeFrame(frame, c.config.AuthKeys)
		if err != nil {
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

func (c *Connection) handleUnencrypted(ctx context.Context, message *mtproto.UnencryptedMessage) error {
	c.mu.Lock()
	bound := len(c.sessions) != 0 || c.authKeyPinned
	c.mu.Unlock()
	if bound || c.authorization != nil {
		return ErrConnectionProtocol
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
		return ErrConnectionProtocol
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
		return nil, ErrConnectionProtocol
	}
	key := session.SessionKey{AuthKeyID: decoded.AuthKeyID, SessionID: inner.SessionID}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.authKeyPinned && c.authKeyID != decoded.AuthKeyID {
		return nil, ErrConnectionProtocol
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
	c.sessions[key] = actor
	actor.start()
	return actor, nil
}

func (c *Connection) removeSession(key session.SessionKey, actor *connectionSession) {
	c.mu.Lock()
	if c.sessions[key] == actor {
		delete(c.sessions, key)
	}
	c.mu.Unlock()
}

func (c *Connection) requestInfo(snapshot session.Snapshot) RequestInfo {
	transportMode := c.config.Transport
	if provider, ok := c.config.Conn.(interface{ TransportMode() string }); ok {
		if negotiated := provider.TransportMode(); negotiated != "" {
			transportMode = negotiated
		}
	}
	info := RequestInfo{
		ConnectionID: c.config.ConnectionID,
		AuthKeyID:    snapshot.AuthKeyID, SessionID: snapshot.SessionID,
		ServerSalt: snapshot.ServerSalt, UserID: snapshot.UserID, Layer: snapshot.Layer,
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

func (c *Connection) nextResponseMessageID() int64 {
	return c.messageIDs.Next()&^int64(3) | 1
}

func (c *Connection) shutdown(cause error) {
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
	return s.writer.Submit(ctx, Push{Body: append([]byte(nil), body...)})
}

var _ Sender = (*requestSender)(nil)
