package runtime

import (
	"context"
	"errors"
	"sync"
	"time"

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
	lease       session.Lease
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

const invokeAfterWaitTimeout = 500 * time.Millisecond

const interruptedRequestRetryMessage = "REQUEST_RETRY"

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
	lease, err := owner.config.Sessions.Acquire(ctx, key, initial)
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
	validator, err := NewSessionValidatorWithLimits(snapshot, owner.now, owner.config.DecodeLimits)
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
		Lease:           lease,
		AuthKey:         decoded.AuthKey,
		Sink:            owner.frameSink,
		MessageIDs:      owner.messageIDs,
		Reliability:     reliability.outboundStore(),
		Now:             owner.now,
		MaxEncodedBytes: owner.config.MaxEncodedBytes,
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
		acceptsPush: snapshot.PushSubscription,
	}
	actor.sender = &requestSender{writer: writer, connectionID: owner.config.ConnectionID}
	if owner.config.Presence != nil {
		owner.config.Presence.Update(snapshot, actor.sender, actor.acceptsPush)
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
			if handled, recoveryErr := s.recoverInterruptedApplicationReplay(ctx, inner, bad); handled {
				return recoveryErr
			}
			intent, encodeErr := badMessageIntent(bad)
			if encodeErr != nil {
				return encodeErr
			}
			return s.writer.Submit(ctx, intent)
		}
		return err
	}
	contentMessageIDs := make([]int64, 0, len(validated.Messages))
	for _, message := range validated.Messages {
		if !message.Retransmission && message.ContentRelated && !isRuntimeControlConstructor(message.ConstructorID) {
			contentMessageIDs = append(contentMessageIDs, message.MessageID)
		}
	}
	reservations, err := s.beginRequests(ctx, contentMessageIDs)
	if err != nil {
		var capacity *ActiveRequestCapacityError
		if errors.As(err, &capacity) {
			return s.rejectOverloaded(ctx, contentMessageIDs)
		}
		return err
	}
	releaseReservations := func() {
		for messageID, reservation := range reservations {
			reservation.Complete(false)
			delete(reservations, messageID)
		}
	}
	if err := s.lease.Save(ctx, validated.Snapshot); err != nil {
		releaseReservations()
		return err
	}
	if err := recordValidated(s.reliability.inboundLedger(), validated); err != nil {
		releaseReservations()
		return err
	}
	for _, message := range validated.Messages {
		if !message.Retransmission && message.ContentRelated {
			if err := s.ensureNewSessionCreated(ctx, message.MessageID); err != nil {
				releaseReservations()
				return err
			}
			break
		}
	}

	handledReplays := make(map[int64]struct{})
	for _, message := range validated.Messages {
		if message.Retransmission {
			if _, handled := handledReplays[message.MessageID]; handled {
				continue
			}
			handledReplays[message.MessageID] = struct{}{}
			if err := s.recoverContainerReplay(ctx, message); err != nil {
				releaseReservations()
				return err
			}
			continue
		}
		reservation := reservations[message.MessageID]
		if reservation != nil {
			delete(reservations, message.MessageID)
		}
		if err := s.routeMessage(ctx, message, reservation); err != nil {
			if reservation != nil {
				reservation.Complete(false)
			}
			releaseReservations()
			return err
		}
	}
	return nil
}

// recoverContainerReplay never routes a retransmitted body. Replay effects
// follow container wire order so preceding acknowledgements are applied first.
// Active requests keep their owner; missing replies produce a correlated retry.
func (s *connectionSession) recoverContainerReplay(ctx context.Context, message InboundMessage) error {
	if !message.ContentRelated {
		return nil
	}
	id := message.MessageID
	if !s.active.IsActive(id) {
		found, err := s.writer.ReplayResponse(ctx, id)
		if err != nil {
			return err
		}
		if !found && !isRuntimeControlConstructor(message.ConstructorID) {
			if err := s.writer.Submit(ctx, RPCError{RequestMessageID: id, Code: 500, Message: interruptedRequestRetryMessage}); err != nil {
				return err
			}
		}
	}
	return s.writer.Submit(ctx, Acknowledge{MessageIDs: []int64{id}})
}

// recoverInterruptedApplicationReplay turns a restart-lost application RPC
// into a correlated retryable failure. Validation is intentionally committed
// before dispatch, so a new process correctly rejects the same client message
// ID as a replay while it has neither the old handler nor its process-local
// writer/reliability record. Re-dispatching would violate at-most-once handler
// execution because the application might already have committed side effects.
//
// A duplicate with an active handler or a locally retained response remains on
// the canonical bad_msg_notification path; the original handler remains the
// sole owner of its correlation.
func (s *connectionSession) recoverInterruptedApplicationReplay(ctx context.Context, inner *mtproto.InnerData, bad *protocol.BadMessageError) (bool, error) {
	if s == nil || inner == nil || bad == nil || !errors.Is(bad.Cause, protocol.ErrReplayMessageID) {
		return false, nil
	}
	if s.active.IsActive(bad.MessageID) || s.reliability.inboundLedger().HasResponse(bad.MessageID) {
		return false, nil
	}

	budget, err := mtproto.NewDecodeBudget(s.owner.config.DecodeLimits)
	if err != nil {
		return false, err
	}
	envelope, messages, err := classifyProtocolMessage(inner, budget)
	if err != nil {
		return false, nil
	}
	replayedContainer := envelope.Kind == protocol.Container && bad.MessageID == inner.MsgID
	intents := make([]Intent, 0, len(messages)+1)
	acknowledged := make([]int64, 0, len(messages))
	for _, message := range messages {
		if !replayedContainer && message.MessageID != bad.MessageID {
			continue
		}
		if !message.ContentRelated || isRuntimeControlConstructor(message.ConstructorID) {
			continue
		}
		if s.active.IsActive(message.MessageID) || s.reliability.inboundLedger().HasResponse(message.MessageID) {
			continue
		}
		intents = append(intents, RPCError{RequestMessageID: message.MessageID, Code: 500, Message: interruptedRequestRetryMessage})
		acknowledged = append(acknowledged, message.MessageID)
	}
	if len(acknowledged) == 0 {
		return false, nil
	}
	intents = append(intents, Acknowledge{MessageIDs: acknowledged})
	return true, s.writer.Submit(ctx, Batch{Items: intents})
}

func (s *connectionSession) routeMessage(ctx context.Context, message InboundMessage, reservation *activeRequestRegistration) error {
	if reservation != nil {
		defer func() {
			if reservation != nil {
				reservation.Complete(false)
			}
		}()
	}
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
	request.Info.Sender = &requestSender{writer: s.writer, suppress: current.SuppressPush, connectionID: s.owner.config.ConnectionID}
	outcome, handled, err := s.router.RouteControl(ctx, request)
	if err != nil {
		return err
	}
	if handled {
		return s.applyOutcome(ctx, current, outcome)
	}
	if !current.SuppressPush {
		if err := s.subscribeForPush(ctx); err != nil {
			return err
		}
	}
	if reservation == nil {
		return ErrConnectionProtocol
	}
	handlerCtx, complete := reservation.Context, reservation.Complete
	reservation = nil
	go func() {
		succeeded := false
		defer func() { complete(succeeded) }()
		if dependencyErr := s.active.WaitDependencies(handlerCtx, current.Dependencies, invokeAfterWaitTimeout); dependencyErr != nil {
			if errors.Is(dependencyErr, context.Canceled) {
				return
			}
			message := "MSG_WAIT_FAILED"
			if errors.Is(dependencyErr, ErrInvokeAfterTimeout) {
				message = "MSG_WAIT_TIMEOUT"
			}
			if submitErr := s.writer.Submit(handlerCtx, RPCError{RequestMessageID: current.MessageID, Code: 500, Message: message}); submitErr != nil && !errors.Is(submitErr, context.Canceled) {
				s.lease.Retire(submitErr)
			}
			return
		}
		outcome, dispatchErr := s.router.DispatchApplication(handlerCtx, request)
		if dispatchErr == nil {
			dispatchErr = s.applyOutcome(handlerCtx, current, outcome)
			succeeded = dispatchErr == nil && requestOutcomeSucceeded(outcome, current.MessageID)
		}
		if dispatchErr != nil && !errors.Is(dispatchErr, context.Canceled) {
			s.lease.Retire(dispatchErr)
		}
	}()
	return nil
}

func (s *connectionSession) beginRequests(ctx context.Context, messageIDs []int64) (map[int64]*activeRequestRegistration, error) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.closing {
		return nil, session.ErrLeaseInactive
	}
	if !s.owner.admission.acquire(len(messageIDs)) {
		return nil, &ActiveRequestCapacityError{Capacity: s.owner.config.ActiveRequests}
	}
	registrations, err := s.active.beginBatch(ctx, messageIDs)
	if err != nil {
		s.owner.admission.release(len(messageIDs))
		return nil, err
	}
	s.requestWG.Add(len(registrations))
	reserved := make(map[int64]*activeRequestRegistration, len(registrations))
	for index, registration := range registrations {
		registration := registration
		var once sync.Once
		complete := func(success bool) {
			once.Do(func() {
				registration.Complete(success)
				s.owner.admission.release(1)
				s.requestWG.Done()
			})
		}
		reserved[messageIDs[index]] = &activeRequestRegistration{Context: registration.Context, Complete: complete}
	}
	return reserved, nil
}

func requestOutcomeSucceeded(outcome Outcome, requestMessageID int64) bool {
	result := false
	failed := false
	var inspect func([]Intent)
	inspect = func(intents []Intent) {
		for _, intent := range intents {
			switch value := intent.(type) {
			case RPCResult:
				if value.RequestMessageID == requestMessageID {
					result = true
				}
			case RPCError:
				if value.RequestMessageID == requestMessageID {
					failed = true
				}
			case Batch:
				inspect(value.Items)
			}
		}
	}
	inspect(outcome.Intents)
	return result && !failed
}

func (s *connectionSession) rejectOverloaded(ctx context.Context, messageIDs []int64) error {
	intents := make([]Intent, 0, len(messageIDs))
	for _, messageID := range messageIDs {
		intents = append(intents, RPCError{RequestMessageID: messageID, Code: 500, Message: "SERVER_BUSY"})
	}
	if len(intents) == 0 {
		return nil
	}
	if len(intents) == 1 {
		return s.writer.Submit(ctx, intents[0])
	}
	return s.writer.Submit(ctx, Batch{Items: intents})
}

func isRuntimeControlConstructor(constructorID uint32) bool {
	switch constructorID {
	case mtprototl.MsgsAckID, mtprototl.MsgsStateReqID, mtprototl.MsgResendReqID,
		mtprototl.RPCDropAnswerID, mtprototl.GetFutureSaltsID:
		return true
	default:
		return false
	}
}

func (s *connectionSession) subscribeForPush(ctx context.Context) error {
	s.outcomeMu.Lock()
	defer s.outcomeMu.Unlock()
	if s.acceptsPush {
		return nil
	}
	snapshot, err := s.lease.Snapshot()
	if err != nil {
		return err
	}
	next, err := ApplySessionMutations(snapshot, []SessionMutation{SetPushSubscription{Enabled: true}})
	if err != nil {
		return err
	}
	if err := s.lease.Save(ctx, next); err != nil {
		return err
	}
	s.acceptsPush = next.PushSubscription
	if s.owner.config.Presence != nil {
		s.owner.config.Presence.Update(next, s.sender, s.acceptsPush)
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
	if err := s.lease.Save(ctx, next); err != nil {
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
	if err := s.lease.Save(ctx, next); err != nil {
		return err
	}
	s.acceptsPush = next.PushSubscription
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
		s.owner.removeSession(s.key, s, cause)
	})
}

func recordValidated(ledger *InboundStateLedger, validated ValidatedInbound) error {
	if validated.Envelope.ConstructorID == mtprototl.MsgContainerID {
		if err := ledger.Record(validated.Envelope); err != nil {
			return err
		}
	}
	for _, message := range validated.Messages {
		if message.Retransmission {
			continue
		}
		if err := ledger.Record(message); err != nil {
			return err
		}
	}
	return nil
}

// connectionRequestAdmission keeps MaxInFlightRequests connection-wide even
// though duplicate lookup and rpc_drop_answer remain session-local.
type connectionRequestAdmission struct {
	mu       sync.Mutex
	capacity int
	inUse    int
}

func newConnectionRequestAdmission(capacity int) *connectionRequestAdmission {
	return &connectionRequestAdmission{capacity: capacity}
}

func (a *connectionRequestAdmission) acquire(count int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if count < 0 || a.inUse+count > a.capacity {
		return false
	}
	a.inUse += count
	return true
}

func (a *connectionRequestAdmission) release(count int) {
	a.mu.Lock()
	a.inUse -= count
	if a.inUse < 0 {
		panic("runtime: released more request admission slots than acquired")
	}
	a.mu.Unlock()
}
