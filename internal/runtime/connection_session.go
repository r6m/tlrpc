package runtime

import (
	"context"
	"errors"
	"sync"

	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/mtproto/protocol"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
)

// connectionSession owns all mutable protocol state for one composite
// (auth-key ID, session ID) attached to a physical connection. The connection
// owns framing and physical writes; actors own leases, validation, routing,
// reliability, request cancellation, push subscription, and outbound session
// progress.
type connectionSession struct {
	owner       *Connection
	key         session.SessionKey
	lease       *SessionLease
	reliability *ReliabilityHandle
	validator   *SessionValidator
	writer      *Writer
	router      *Router
	active      *ActiveRequestRegistry
	sender      *requestSender

	outcomeMu   sync.Mutex
	acceptsPush bool

	lifecycleMu  sync.Mutex
	closing      bool
	requestWG    sync.WaitGroup
	shutdownOnce sync.Once
}

func newConnectionSession(ctx context.Context, owner *Connection, decoded DecodedFrame) (*connectionSession, error) {
	inner := decoded.Encrypted
	if owner == nil || inner == nil {
		return nil, ErrConnectionConfig
	}
	key := session.SessionKey{AuthKeyID: decoded.AuthKeyID, SessionID: inner.SessionID}
	salt := inner.Salt
	if owner.authorization != nil {
		salt = owner.authorization.InitialServerSalt
	}
	now := owner.now().UTC()
	initial := session.Snapshot{
		AuthKeyID:    decoded.AuthKeyID,
		SessionID:    inner.SessionID,
		ServerSalt:   salt,
		CreatedAt:    now,
		LastActivity: now,
	}
	lease, err := owner.config.Leases.Acquire(ctx, key, initial)
	if err != nil {
		return nil, err
	}
	releaseLease := true
	defer func() {
		if releaseLease {
			lease.Release()
		}
	}()

	snapshot, err := lease.Snapshot()
	if err != nil {
		return nil, err
	}
	validator, err := NewSessionValidator(snapshot, owner.now)
	if err != nil {
		return nil, err
	}
	reliability, err := owner.config.Reliability.Acquire(key)
	if err != nil {
		return nil, err
	}
	releaseReliability := true
	defer func() {
		if releaseReliability {
			reliability.Release()
		}
	}()

	active, err := NewActiveRequestRegistry(owner.config.ActiveRequests)
	if err != nil {
		return nil, err
	}
	writer, err := NewWriter(lease.Context(), WriterConfig{
		Lease:       lease,
		AuthKey:     decoded.AuthKey,
		Sink:        owner.frameSink,
		MessageIDs:  owner.messageIDs,
		Reliability: reliability.outboundStore(),
		Now:         owner.now,
	})
	if err != nil {
		return nil, err
	}
	controls, err := NewMTProtoControlRouter(MTProtoControlConfig{
		Outbound: writer,
		Inbound:  reliability.inboundLedger(),
		Active:   active,
		Now:      owner.now,
	})
	if err != nil {
		lease.Retire(err)
		<-writer.Done()
		return nil, err
	}
	router, err := NewRouter(controls, owner.config.Application)
	if err != nil {
		lease.Retire(err)
		<-writer.Done()
		return nil, err
	}

	actor := &connectionSession{
		owner:       owner,
		key:         key,
		lease:       lease,
		reliability: reliability,
		validator:   validator,
		writer:      writer,
		router:      router,
		active:      active,
	}
	actor.sender = &requestSender{writer: writer}
	if owner.config.Presence != nil {
		owner.config.Presence.Update(snapshot, actor.sender, false)
	}
	releaseLease = false
	releaseReliability = false
	return actor, nil
}

func (s *connectionSession) start() {
	go func() {
		<-s.lease.Context().Done()
		s.shutdown(context.Cause(s.lease.Context()))
	}()
}

func (s *connectionSession) handleEncrypted(ctx context.Context, inner *mtproto.InnerData) error {
	if inner == nil || inner.SessionID != s.key.SessionID {
		return ErrConnectionProtocol
	}
	snapshot, err := s.lease.Snapshot()
	if err != nil {
		return err
	}
	validated, err := s.validator.Validate(snapshot, inner)
	if err != nil {
		var bad *protocol.BadMessageError
		if errors.As(err, &bad) {
			intent, encodeErr := badMessageIntent(bad)
			if encodeErr != nil {
				return encodeErr
			}
			return s.writer.Submit(ctx, intent)
		}
		return err
	}
	if err := s.lease.Commit(ctx, validated.Snapshot); err != nil {
		return err
	}
	if err := recordValidated(s.reliability.inboundLedger(), validated); err != nil {
		return err
	}
	for _, message := range validated.Messages {
		if message.ContentRelated {
			if err := s.ensureNewSessionCreated(ctx, message.MessageID); err != nil {
				return err
			}
			break
		}
	}

	for _, message := range validated.Messages {
		if err := s.routeMessage(ctx, message); err != nil {
			return err
		}
	}
	return nil
}

func (s *connectionSession) routeMessage(ctx context.Context, message InboundMessage) error {
	snapshot, err := s.lease.Snapshot()
	if err != nil {
		return err
	}
	request := Request{Message: message, Info: s.owner.requestInfo(snapshot)}
	request, wrapperMutations, err := NormalizeRequest(request, WrapperConfig{
		SchemaLayer:       s.owner.config.SchemaLayer,
		MaxDecodedPayload: s.owner.config.MaxDecodedPayload,
	})
	if err != nil {
		return err
	}
	if len(wrapperMutations) != 0 {
		if err := s.applyMutations(ctx, wrapperMutations); err != nil {
			return err
		}
		snapshot, err = s.lease.Snapshot()
		if err != nil {
			return err
		}
		request.Info = s.owner.requestInfo(snapshot)
	}
	current := request.Message
	request.Info.Sender = &requestSender{writer: s.writer, suppress: current.SuppressPush}
	outcome, handled, err := s.router.RouteControl(ctx, request)
	if err != nil {
		return err
	}
	if handled {
		return s.applyOutcome(ctx, current, outcome)
	}
	if !current.SuppressPush {
		if err := s.subscribeForPush(); err != nil {
			return err
		}
	}
	handlerCtx, complete, err := s.beginRequest(ctx, current.MessageID)
	if err != nil {
		return err
	}
	go func() {
		defer complete()
		outcome, dispatchErr := s.router.DispatchApplication(handlerCtx, request)
		if dispatchErr == nil {
			dispatchErr = s.applyOutcome(handlerCtx, current, outcome)
		}
		if dispatchErr != nil && !errors.Is(dispatchErr, context.Canceled) {
			s.lease.Retire(dispatchErr)
		}
	}()
	return nil
}

func (s *connectionSession) beginRequest(ctx context.Context, messageID int64) (context.Context, func(), error) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.closing {
		return nil, nil, ErrSessionLeaseInactive
	}
	if !s.owner.admission.acquire() {
		return nil, nil, &ActiveRequestCapacityError{Capacity: s.owner.config.ActiveRequests}
	}
	handlerCtx, completeActive, err := s.active.Begin(ctx, messageID)
	if err != nil {
		s.owner.admission.release()
		return nil, nil, err
	}
	s.requestWG.Add(1)
	var once sync.Once
	complete := func() {
		once.Do(func() {
			completeActive()
			s.owner.admission.release()
			s.requestWG.Done()
		})
	}
	return handlerCtx, complete, nil
}

func (s *connectionSession) subscribeForPush() error {
	s.outcomeMu.Lock()
	defer s.outcomeMu.Unlock()
	if s.acceptsPush {
		return nil
	}
	snapshot, err := s.lease.Snapshot()
	if err != nil {
		return err
	}
	s.acceptsPush = true
	if s.owner.config.Presence != nil {
		s.owner.config.Presence.Update(snapshot, s.sender, true)
	}
	return nil
}

func (s *connectionSession) ensureNewSessionCreated(ctx context.Context, firstMessageID int64) error {
	s.outcomeMu.Lock()
	defer s.outcomeMu.Unlock()
	snapshot, err := s.lease.Snapshot()
	if err != nil || snapshot.NewSessionCreated {
		return err
	}
	next, err := ApplySessionMutations(snapshot, []SessionMutation{MarkNewSessionCreated{FirstMessageID: firstMessageID}})
	if err != nil {
		return err
	}
	if err := s.lease.Commit(ctx, next); err != nil {
		return err
	}
	body, err := serializeRuntimeTL(&mtprototl.NewSessionCreated{
		FirstMsgID: firstMessageID,
		UniqueID:   s.owner.now().UnixNano(),
		ServerSalt: snapshot.ServerSalt,
	})
	if err != nil {
		return err
	}
	return s.writer.Submit(ctx, ProtocolReply{Body: body, Unsolicited: true})
}

func (s *connectionSession) applyOutcome(ctx context.Context, message InboundMessage, outcome Outcome) error {
	s.outcomeMu.Lock()
	defer s.outcomeMu.Unlock()
	if err := ValidateOutcome(outcome); err != nil {
		return err
	}
	if message.SuppressPush {
		outcome.Intents = suppressPushIntents(outcome.Intents)
	}
	if len(outcome.Mutations) != 0 {
		if err := s.applyMutationsLocked(ctx, outcome.Mutations); err != nil {
			return err
		}
	}
	for _, intent := range outcome.Intents {
		if err := s.writer.Submit(ctx, intent); err != nil {
			return err
		}
	}
	acknowledged := false
	if message.ContentRelated {
		if err := s.writer.Submit(ctx, Acknowledge{MessageIDs: []int64{message.MessageID}}); err != nil {
			return err
		}
		acknowledged = true
	}
	s.reliability.inboundLedger().Complete([]int64{message.MessageID}, acknowledged, len(outcome.Intents) != 0)
	return nil
}

func (s *connectionSession) applyMutations(ctx context.Context, mutations []SessionMutation) error {
	s.outcomeMu.Lock()
	defer s.outcomeMu.Unlock()
	return s.applyMutationsLocked(ctx, mutations)
}

func (s *connectionSession) applyMutationsLocked(ctx context.Context, mutations []SessionMutation) error {
	snapshot, err := s.lease.Snapshot()
	if err != nil {
		return err
	}
	next, err := ApplySessionMutations(snapshot, mutations)
	if err != nil {
		return err
	}
	if err := s.lease.Commit(ctx, next); err != nil {
		return err
	}
	if s.owner.config.Presence != nil {
		s.owner.config.Presence.Update(next, s.sender, s.acceptsPush)
	}
	return nil
}

func (s *connectionSession) shutdown(cause error) {
	s.shutdownOnce.Do(func() {
		if cause == nil {
			cause = context.Canceled
		}
		s.lifecycleMu.Lock()
		s.closing = true
		s.lifecycleMu.Unlock()
		if s.owner.config.Presence != nil {
			s.owner.config.Presence.Remove(s.key, s.sender)
		}
		s.active.CancelAll(cause)
		s.lease.Retire(cause)
		s.requestWG.Wait()
		<-s.writer.Done()
		s.lease.Release()
		s.reliability.Release()
		s.owner.removeSession(s.key, s)
	})
}

func recordValidated(ledger *InboundStateLedger, validated ValidatedInbound) error {
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

// connectionRequestAdmission keeps MaxInFlightRequests connection-wide even
// though duplicate lookup and rpc_drop_answer remain session-local.
type connectionRequestAdmission struct {
	slots chan struct{}
}

func newConnectionRequestAdmission(capacity int) *connectionRequestAdmission {
	return &connectionRequestAdmission{slots: make(chan struct{}, capacity)}
}

func (a *connectionRequestAdmission) acquire() bool {
	select {
	case a.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (a *connectionRequestAdmission) release() {
	<-a.slots
}
