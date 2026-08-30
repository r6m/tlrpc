package runtime

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/r6m/tlrpc/internal/handshake"
	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/mtproto/protocol"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
)

const DefaultActiveRequestCapacity = 1024

var (
	ErrConnectionConfig         = errors.New("runtime: incomplete connection configuration")
	ErrConnectionProtocol       = errors.New("runtime: invalid connection protocol transition")
	ErrHandshakeAuthKeyMismatch = errors.New("runtime: encrypted auth key differs from completed handshake")
	ErrUnknownSessionProgress   = errors.New("runtime: unknown session starts after its initial sequence")
)

// FrameConnection is the transport-neutral message boundary consumed by
// Runtime v2. TCP and WebSocket adapters both satisfy this contract.
type FrameConnection interface {
	ReadMessage(maxPayloadBytes int) ([]byte, error)
	WriteMessage(frame []byte) error
	Close() error
	Context() context.Context
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
}

type ConnectionConfig struct {
	Conn              FrameConnection
	AuthKeys          AuthKeySource
	Handshake         *handshake.Engine
	Leases            *SessionLeaseRegistry
	Reliability       *ReliabilityRegistry
	Application       ApplicationDispatcher
	MessageIDs        MessageIDSource
	MaxPayloadBytes   int
	MaxDecodedPayload int
	ActiveRequests    int
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

	mu                sync.Mutex
	lease             *SessionLease
	reliability       *ReliabilityHandle
	validator         *SessionValidator
	writer            *Writer
	router            *Router
	active            *ActiveRequestRegistry
	sender            *requestSender
	acceptsPush       bool
	requestWG         sync.WaitGroup
	outcomeMu         sync.Mutex
	receivedEncrypted bool

	handshakeSession *handshake.Session
	authorization    *handshake.Result
}

func NewConnection(config ConnectionConfig) (*Connection, error) {
	if config.Conn == nil || config.AuthKeys == nil || config.Handshake == nil ||
		config.Leases == nil || config.Reliability == nil || config.Application == nil {
		return nil, ErrConnectionConfig
	}
	if config.ActiveRequests == 0 {
		config.ActiveRequests = DefaultActiveRequestCapacity
	}
	if config.ActiveRequests < 0 {
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
	return &Connection{config: config, messageIDs: messageIDs, now: now}, nil
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
	bound := c.lease != nil
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
	c.mu.Lock()
	existingLease := c.lease
	existingWriter := c.writer
	c.mu.Unlock()
	if existingLease != nil && existingLease.Key() != (session.SessionKey{AuthKeyID: decoded.AuthKeyID, SessionID: inner.SessionID}) {
		if existingLease.Key().AuthKeyID != decoded.AuthKeyID {
			return ErrConnectionProtocol
		}
		bad := &protocol.BadMessageError{
			MessageID: inner.MsgID, SequenceNo: inner.SeqNo,
			Code: protocol.CodeSessionIDMismatch, Cause: protocol.ErrSessionIDMismatch,
		}
		intent, err := badMessageIntent(bad)
		if err != nil {
			return err
		}
		return existingWriter.Submit(ctx, intent)
	}
	if err := c.bind(ctx, decoded); err != nil {
		return err
	}

	c.mu.Lock()
	validator := c.validator
	lease := c.lease
	ledger := c.reliability.inboundLedger()
	writer := c.writer
	router := c.router
	active := c.active
	firstEncrypted := !c.receivedEncrypted
	c.receivedEncrypted = true
	c.mu.Unlock()
	if firstEncrypted && lease.Created() && inner.SeqNo > 1 {
		bad := &protocol.BadMessageError{
			MessageID: inner.MsgID, SequenceNo: inner.SeqNo,
			Code: protocol.CodeSessionIDMismatch, Cause: protocol.ErrSessionIDMismatch,
		}
		intent, err := badMessageIntent(bad)
		if err != nil {
			return err
		}
		if err := writer.Submit(ctx, intent); err != nil {
			return err
		}
		return lease.Delete(ctx)
	}

	snapshot, err := lease.Snapshot()
	if err != nil {
		return err
	}
	validated, err := validator.Validate(snapshot, inner)
	if err != nil {
		var bad *protocol.BadMessageError
		if errors.As(err, &bad) {
			intent, encodeErr := badMessageIntent(bad)
			if encodeErr != nil {
				return encodeErr
			}
			return writer.Submit(ctx, intent)
		}
		return err
	}
	if err := lease.Commit(ctx, validated.Snapshot); err != nil {
		return err
	}
	if err := c.recordValidated(ledger, validated); err != nil {
		return err
	}
	for _, message := range validated.Messages {
		if message.ContentRelated {
			if err := c.ensureNewSessionCreated(ctx, message.MessageID); err != nil {
				return err
			}
			break
		}
	}

	for _, message := range validated.Messages {
		current := message
		snapshot, err := lease.Snapshot()
		if err != nil {
			return err
		}
		request := Request{Message: current, Info: c.requestInfo(snapshot)}
		request, wrapperMutations, err := NormalizeRequest(request, WrapperConfig{
			SchemaLayer: c.config.SchemaLayer, MaxDecodedPayload: c.config.MaxDecodedPayload,
		})
		if err != nil {
			return err
		}
		if len(wrapperMutations) != 0 {
			if err := c.applyMutations(ctx, wrapperMutations); err != nil {
				return err
			}
			snapshot, err = lease.Snapshot()
			if err != nil {
				return err
			}
			request.Info = c.requestInfo(snapshot)
		}
		current = request.Message
		request.Info.Sender = &requestSender{writer: writer, suppress: current.SuppressPush}
		outcome, handled, err := router.RouteControl(ctx, request)
		if err != nil {
			return err
		}
		if handled {
			if err := c.applyOutcome(ctx, current, outcome); err != nil {
				return err
			}
			continue
		}
		c.setPushSubscription(snapshot, !current.SuppressPush)

		handlerCtx, complete, err := active.Begin(ctx, current.MessageID)
		if err != nil {
			return err
		}
		c.requestWG.Add(1)
		go func() {
			defer c.requestWG.Done()
			defer complete()
			outcome, dispatchErr := router.DispatchApplication(handlerCtx, request)
			if dispatchErr == nil {
				dispatchErr = c.applyOutcome(handlerCtx, current, outcome)
			}
			if dispatchErr != nil && !errors.Is(dispatchErr, context.Canceled) {
				lease.Retire(dispatchErr)
			}
		}()
	}
	return nil
}

func (c *Connection) bind(ctx context.Context, decoded DecodedFrame) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	inner := decoded.Encrypted
	key := session.SessionKey{AuthKeyID: decoded.AuthKeyID, SessionID: inner.SessionID}
	if c.lease != nil {
		if c.lease.Key() != key {
			return ErrConnectionProtocol
		}
		return nil
	}

	salt := inner.Salt
	if c.authorization != nil {
		salt = c.authorization.InitialServerSalt
	}
	now := c.now().UTC()
	initial := session.Snapshot{
		AuthKeyID: decoded.AuthKeyID, SessionID: inner.SessionID,
		ServerSalt: salt, CreatedAt: now, LastActivity: now,
	}
	lease, err := c.config.Leases.Acquire(ctx, key, initial)
	if err != nil {
		return err
	}
	snapshot, err := lease.Snapshot()
	if err != nil {
		lease.Release()
		return err
	}
	validator, err := NewSessionValidator(snapshot, c.now)
	if err != nil {
		lease.Release()
		return err
	}
	reliabilityHandle, err := c.config.Reliability.Acquire(key)
	if err != nil {
		lease.Release()
		return err
	}
	active, err := NewActiveRequestRegistry(c.config.ActiveRequests)
	if err != nil {
		reliabilityHandle.Release()
		lease.Release()
		return err
	}
	writer, err := NewWriter(lease.Context(), WriterConfig{
		Lease: lease, AuthKey: decoded.AuthKey,
		Sink:       &connectionFrameSink{connection: c.config.Conn},
		MessageIDs: c.messageIDs, Reliability: reliabilityHandle.outboundStore(), Now: c.now,
	})
	if err != nil {
		reliabilityHandle.Release()
		lease.Release()
		return err
	}
	controls, err := NewMTProtoControlRouter(MTProtoControlConfig{
		Outbound: writer, Inbound: reliabilityHandle.inboundLedger(), Active: active, Now: c.now,
	})
	if err != nil {
		lease.Retire(err)
		reliabilityHandle.Release()
		lease.Release()
		return err
	}
	router, err := NewRouter(controls, c.config.Application)
	if err != nil {
		lease.Retire(err)
		reliabilityHandle.Release()
		lease.Release()
		return err
	}
	c.lease = lease
	c.reliability = reliabilityHandle
	c.validator = validator
	c.writer = writer
	c.sender = &requestSender{writer: writer}
	c.acceptsPush = true
	c.router = router
	c.active = active
	if c.config.Presence != nil {
		c.config.Presence.Update(snapshot, c.sender, true)
	}
	return nil
}

func (c *Connection) recordValidated(ledger *InboundStateLedger, validated ValidatedInbound) error {
	if validated.Envelope.ConstructorID == mtprototl.MsgContainerID {
		if err := ledger.Record(validated.Envelope); err != nil {
			return err
		}
	}
	for _, message := range validated.Messages {
		if err := ledger.Record(message); err != nil {
			return err
		}
	}
	return nil
}

func (c *Connection) ensureNewSessionCreated(ctx context.Context, firstMessageID int64) error {
	c.outcomeMu.Lock()
	defer c.outcomeMu.Unlock()
	snapshot, err := c.lease.Snapshot()
	if err != nil || snapshot.NewSessionCreated {
		return err
	}
	next, err := ApplySessionMutations(snapshot, []SessionMutation{MarkNewSessionCreated{FirstMessageID: firstMessageID}})
	if err != nil {
		return err
	}
	if err := c.lease.Commit(ctx, next); err != nil {
		return err
	}
	body, err := serializeRuntimeTL(&mtprototl.NewSessionCreated{
		FirstMsgID: firstMessageID, UniqueID: c.now().UnixNano(), ServerSalt: snapshot.ServerSalt,
	})
	if err != nil {
		return err
	}
	return c.writer.Submit(ctx, ProtocolReply{Body: body, Unsolicited: true})
}

func (c *Connection) applyOutcome(ctx context.Context, message InboundMessage, outcome Outcome) error {
	c.outcomeMu.Lock()
	defer c.outcomeMu.Unlock()
	if err := ValidateOutcome(outcome); err != nil {
		return err
	}
	if message.SuppressPush {
		outcome.Intents = suppressPushIntents(outcome.Intents)
	}
	if len(outcome.Mutations) != 0 {
		if err := c.applyMutationsLocked(ctx, outcome.Mutations); err != nil {
			return err
		}
	}
	for _, intent := range outcome.Intents {
		if err := c.writer.Submit(ctx, intent); err != nil {
			return err
		}
	}
	acknowledged := false
	if message.ContentRelated {
		if err := c.writer.Submit(ctx, Acknowledge{MessageIDs: []int64{message.MessageID}}); err != nil {
			return err
		}
		acknowledged = true
	}
	c.reliability.inboundLedger().Complete([]int64{message.MessageID}, acknowledged, len(outcome.Intents) != 0)
	return nil
}

func (c *Connection) applyMutations(ctx context.Context, mutations []SessionMutation) error {
	c.outcomeMu.Lock()
	defer c.outcomeMu.Unlock()
	return c.applyMutationsLocked(ctx, mutations)
}

func (c *Connection) applyMutationsLocked(ctx context.Context, mutations []SessionMutation) error {
	snapshot, err := c.lease.Snapshot()
	if err != nil {
		return err
	}
	next, err := ApplySessionMutations(snapshot, mutations)
	if err != nil {
		return err
	}
	if err := c.lease.Commit(ctx, next); err != nil {
		return err
	}
	if c.config.Presence != nil {
		c.mu.Lock()
		acceptsPush := c.acceptsPush
		c.mu.Unlock()
		c.config.Presence.Update(next, c.sender, acceptsPush)
	}
	return nil
}

func (c *Connection) setPushSubscription(snapshot session.Snapshot, acceptsPush bool) {
	c.mu.Lock()
	c.acceptsPush = acceptsPush
	sender := c.sender
	c.mu.Unlock()
	if c.config.Presence != nil && sender != nil {
		c.config.Presence.Update(snapshot, sender, acceptsPush)
	}
}

func (c *Connection) requestInfo(snapshot session.Snapshot) RequestInfo {
	transportMode := c.config.Transport
	if provider, ok := c.config.Conn.(interface{ TransportMode() string }); ok {
		if negotiated := provider.TransportMode(); negotiated != "" {
			transportMode = negotiated
		}
	}
	info := RequestInfo{
		AuthKeyID: snapshot.AuthKeyID, SessionID: snapshot.SessionID,
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
	active := c.active
	lease := c.lease
	reliabilityHandle := c.reliability
	sender := c.sender
	handshakeSession := c.handshakeSession
	c.mu.Unlock()
	if c.config.Presence != nil && lease != nil && sender != nil {
		c.config.Presence.Remove(lease.Key(), sender)
	}
	if active != nil {
		active.CancelAll(cause)
	}
	if lease != nil {
		lease.Retire(cause)
	}
	c.requestWG.Wait()
	if lease != nil {
		lease.Release()
	}
	if reliabilityHandle != nil {
		reliabilityHandle.Release()
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

type connectionFrameSink struct{ connection FrameConnection }

func (s *connectionFrameSink) WriteFrame(ctx context.Context, frame []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.connection.WriteMessage(frame)
}

func (s *connectionFrameSink) Close() error { return s.connection.Close() }

var _ FrameSink = (*connectionFrameSink)(nil)

type requestSender struct {
	writer   *Writer
	suppress bool
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
