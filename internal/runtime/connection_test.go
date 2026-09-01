package runtime

import (
	"context"
	"errors"
	"io"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/internal/handshake"
	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
)

func TestConnectionRoutesGeneratedApplicationThroughSingleWriter(t *testing.T) {
	now := time.Unix(inboundNowSeconds, 0).UTC()
	requestID := inboundMessageID(4)
	requestBody := constructorBody(0x10101010)
	application := &connectionApplicationStub{outcome: Outcome{Intents: []Intent{
		RPCResult{RequestMessageID: requestID, Body: constructorBody(0x20202020)},
	}}, pushBody: constructorBody(0x21212121)}
	harness := newConnectionHarness(t, now, application, 4, []mtproto.InnerData{{
		Salt: inboundSalt, SessionID: inboundSessionID,
		MsgID: requestID, SeqNo: 1, Data: requestBody,
	}})

	err := harness.connection.Run(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("Run() error = %v, want EOF", err)
	}
	frames := harness.transport.writtenFrames()
	if len(frames) != 4 {
		t.Fatalf("written frames = %d, want 4", len(frames))
	}
	decoded := make([]*mtproto.InnerData, len(frames))
	for index, frame := range frames {
		decoded[index] = decryptWriterFrame(t, harness.authKey, frame)
	}
	constructors := []uint32{
		binaryConstructor(decoded[0].Data),
		binaryConstructor(decoded[1].Data),
		binaryConstructor(decoded[2].Data), binaryConstructor(decoded[3].Data),
	}
	wantConstructors := []uint32{mtprototl.NewSessionCreatedID, 0x21212121, mtprototl.RPCResultID, mtprototl.MsgsAckID}
	if !reflect.DeepEqual(constructors, wantConstructors) {
		t.Fatalf("wire constructors = %08x, want %08x", constructors, wantConstructors)
	}
	if got := []int32{decoded[0].SeqNo, decoded[1].SeqNo, decoded[2].SeqNo, decoded[3].SeqNo}; !reflect.DeepEqual(got, []int32{0, 1, 3, 4}) {
		t.Fatalf("wire sequence numbers = %v", got)
	}
	if decoded[0].MsgID&3 != 3 || decoded[1].MsgID&3 != 3 || decoded[2].MsgID&3 != 1 || decoded[3].MsgID&3 != 1 {
		t.Fatalf("wire message ID classes = %d, %d, %d, %d", decoded[0].MsgID&3, decoded[1].MsgID&3, decoded[2].MsgID&3, decoded[3].MsgID&3)
	}
	result := &mtprototl.RPCResult{}
	if err := decodeControl(decoded[2].Data, result); err != nil {
		t.Fatal(err)
	}
	if result.ReqMsgID != requestID || binaryConstructor(result.ResultRaw) != 0x20202020 {
		t.Fatalf("rpc result = %+v", result)
	}
	ack := &mtprototl.MsgsAck{}
	if err := decodeControl(decoded[3].Data, ack); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ack.MsgIDs, []int64{requestID}) {
		t.Fatalf("acknowledged request IDs = %v", ack.MsgIDs)
	}
	if application.request.Info.AuthKeyID != harness.authKey.ID() || application.request.Info.SessionID != inboundSessionID || application.request.Info.Peer.Transport != "test" {
		t.Fatalf("application request info = %+v", application.request.Info)
	}
	stored, loadErr := harness.store.Load(context.Background(), session.SessionKey{AuthKeyID: harness.authKey.ID(), SessionID: inboundSessionID})
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if !stored.NewSessionCreated || stored.FirstClientMsgID != requestID || stored.SeqNo != 2 || stored.ServerSeqNo != 2 {
		t.Fatalf("stored session = %+v", stored)
	}
}

func TestConnectionInvokeWithoutUpdatesKeepsBoundSessionSubscribed(t *testing.T) {
	const (
		bindConstructor       = uint32(0x31313131)
		normalConstructor     = uint32(0x32323232)
		suppressedConstructor = uint32(0x33333333)
		incidentalSenderPush  = uint32(0x41414141)
		incidentalOutcomePush = uint32(0x42424242)
		laterServerPush       = uint32(0x43434343)
	)
	now := time.Unix(inboundNowSeconds, 0).UTC()
	application := &subscriptionApplicationStub{
		bindConstructor:       bindConstructor,
		suppressedConstructor: suppressedConstructor,
		incidentalSenderPush:  incidentalSenderPush,
		incidentalOutcomePush: incidentalOutcomePush,
	}
	presence := newSubscriptionPresenceStub()
	harness := newConnectionHarness(t, now, application, 100, nil)
	harness.connection.config.Presence = presence

	handle := func(messageID int64, sequence int32, body []byte) {
		t.Helper()
		err := harness.connection.handleEncrypted(context.Background(), DecodedFrame{
			Encrypted: &mtproto.InnerData{
				Salt: inboundSalt, SessionID: inboundSessionID,
				MsgID: messageID, SeqNo: sequence, Data: body,
			},
			AuthKeyID: harness.authKey.ID(), AuthKey: harness.authKey,
		})
		if err != nil {
			t.Fatalf("handleEncrypted(%08x): %v", binaryConstructor(body), err)
		}
	}

	handle(inboundMessageID(4), 1, constructorBody(bindConstructor))
	presence.waitForUser(t, 42)
	waitForWrittenFrames(t, harness.transport, 3)

	handle(inboundMessageID(8), 3, constructorBody(normalConstructor))
	waitForWrittenFrames(t, harness.transport, 5)

	withoutUpdates := encodeControlBody(t, &mtprototl.InvokeWithoutUpdates{
		QueryRaw: constructorBody(suppressedConstructor),
	})
	handle(inboundMessageID(12), 5, withoutUpdates)
	waitForWrittenFrames(t, harness.transport, 7)

	if err := presence.publish(context.Background(), 42, constructorBody(laterServerPush)); err != nil {
		t.Fatalf("publish after invokeWithoutUpdates: %v", err)
	}
	waitForWrittenFrames(t, harness.transport, 8)

	constructors := make([]uint32, 0, 8)
	for _, frame := range harness.transport.writtenFrames() {
		constructors = append(constructors, binaryConstructor(decryptWriterFrame(t, harness.authKey, frame).Data))
	}
	if containsConstructor(constructors, incidentalSenderPush) {
		t.Fatalf("request sender push escaped invokeWithoutUpdates suppression: %08x", constructors)
	}
	if containsConstructor(constructors, incidentalOutcomePush) {
		t.Fatalf("request outcome push escaped invokeWithoutUpdates suppression: %08x", constructors)
	}
	if !containsConstructor(constructors, laterServerPush) {
		t.Fatalf("later server push was not delivered: %08x", constructors)
	}

	harness.connection.shutdown(io.EOF)
}

func TestConnectionInvokeWithoutUpdatesDoesNotSubscribeColdBoundSession(t *testing.T) {
	const (
		bindConstructor   = uint32(0x61616161)
		normalConstructor = uint32(0x62626262)
		laterServerPush   = uint32(0x63636363)
	)
	now := time.Unix(inboundNowSeconds, 0).UTC()
	application := &subscriptionApplicationStub{bindConstructor: bindConstructor}
	presence := newSubscriptionPresenceStub()
	harness := newConnectionHarness(t, now, application, 100, nil)
	harness.connection.config.Presence = presence

	handle := func(messageID int64, sequence int32, body []byte) {
		t.Helper()
		if err := harness.connection.handleEncrypted(context.Background(), DecodedFrame{
			Encrypted: &mtproto.InnerData{
				Salt: inboundSalt, SessionID: inboundSessionID,
				MsgID: messageID, SeqNo: sequence, Data: body,
			},
			AuthKeyID: harness.authKey.ID(), AuthKey: harness.authKey,
		}); err != nil {
			t.Fatalf("handleEncrypted(%08x): %v", binaryConstructor(body), err)
		}
	}

	handle(inboundMessageID(4), 1, encodeControlBody(t, &mtprototl.InvokeWithoutUpdates{
		QueryRaw: constructorBody(bindConstructor),
	}))
	waitForWrittenFrames(t, harness.transport, 3)
	if err := presence.publish(context.Background(), 42, constructorBody(laterServerPush)); err == nil {
		t.Fatal("cold invokeWithoutUpdates connection unexpectedly subscribed for push")
	}

	handle(inboundMessageID(8), 3, constructorBody(normalConstructor))
	waitForWrittenFrames(t, harness.transport, 5)
	if err := presence.publish(context.Background(), 42, constructorBody(laterServerPush)); err != nil {
		t.Fatalf("publish after normal request: %v", err)
	}
	waitForWrittenFrames(t, harness.transport, 6)

	harness.connection.shutdown(io.EOF)
}

func TestConnectionRestoresDurablePushSubscriptionAcrossReconnect(t *testing.T) {
	const (
		bindConstructor       = uint32(0x71717171)
		suppressedConstructor = uint32(0x72727272)
		incidentalSenderPush  = uint32(0x73737373)
		incidentalOutcomePush = uint32(0x74747474)
		laterServerPush       = uint32(0x75757575)
	)
	now := time.Unix(inboundNowSeconds, 0).UTC()
	application := &subscriptionApplicationStub{
		bindConstructor:       bindConstructor,
		suppressedConstructor: suppressedConstructor,
		incidentalSenderPush:  incidentalSenderPush,
		incidentalOutcomePush: incidentalOutcomePush,
	}
	presence := newSubscriptionPresenceStub()
	store := session.NewMemoryStore()
	first := newConnectionHarnessWithStore(t, now, application, 100, nil, store)
	first.connection.config.Presence = presence

	if err := first.connection.handleEncrypted(context.Background(), DecodedFrame{
		Encrypted: &mtproto.InnerData{
			Salt: inboundSalt, SessionID: inboundSessionID,
			MsgID: inboundMessageID(4), SeqNo: 1, Data: constructorBody(bindConstructor),
		},
		AuthKeyID: first.authKey.ID(), AuthKey: first.authKey,
	}); err != nil {
		t.Fatalf("initial subscribed request: %v", err)
	}
	presence.waitForUser(t, 42)
	waitForWrittenFrames(t, first.transport, 3)
	snapshot := loadConnectionSessionSnapshot(t, store, first.authKey.ID(), inboundSessionID)
	if !snapshot.PushSubscription {
		t.Fatalf("initial session did not persist push subscription: %+v", snapshot)
	}
	first.connection.shutdown(io.EOF)

	second := newConnectionHarnessWithStore(t, now, application, 100, nil, store)
	second.connection.config.Presence = presence
	defer second.connection.shutdown(io.EOF)
	if _, err := second.connection.sessionFor(context.Background(), DecodedFrame{
		Encrypted: &mtproto.InnerData{Salt: inboundSalt, SessionID: inboundSessionID},
		AuthKeyID: second.authKey.ID(), AuthKey: second.authKey,
	}); err != nil {
		t.Fatalf("reconnect session: %v", err)
	}
	presence.waitForUser(t, 42)

	if err := second.connection.handleEncrypted(context.Background(), DecodedFrame{
		Encrypted: &mtproto.InnerData{
			Salt: inboundSalt, SessionID: inboundSessionID,
			MsgID: inboundMessageID(8), SeqNo: 3,
			Data: encodeControlBody(t, &mtprototl.InvokeWithoutUpdates{QueryRaw: constructorBody(suppressedConstructor)}),
		},
		AuthKeyID: second.authKey.ID(), AuthKey: second.authKey,
	}); err != nil {
		t.Fatalf("suppressed request after reconnect: %v", err)
	}
	waitForWrittenFrames(t, second.transport, 2)
	if err := presence.publish(context.Background(), 42, constructorBody(laterServerPush)); err != nil {
		t.Fatalf("restored subscription push: %v", err)
	}
	waitForWrittenFrames(t, second.transport, 3)

	constructors := make([]uint32, 0, 3)
	for _, frame := range second.transport.writtenFrames() {
		constructors = append(constructors, binaryConstructor(decryptWriterFrame(t, second.authKey, frame).Data))
	}
	if containsConstructor(constructors, incidentalSenderPush) || containsConstructor(constructors, incidentalOutcomePush) {
		t.Fatalf("invokeWithoutUpdates leaked suppressed push after reconnect: %08x", constructors)
	}
	if !containsConstructor(constructors, laterServerPush) {
		t.Fatalf("restored subscription did not receive later push: %08x", constructors)
	}
}

func TestConnectionDoesNotRestorePushSubscriptionForColdSession(t *testing.T) {
	const bindConstructor = uint32(0x81818181)
	now := time.Unix(inboundNowSeconds, 0).UTC()
	application := &subscriptionApplicationStub{bindConstructor: bindConstructor}
	presence := newSubscriptionPresenceStub()
	store := session.NewMemoryStore()
	first := newConnectionHarnessWithStore(t, now, application, 100, nil, store)
	first.connection.config.Presence = presence
	if err := first.connection.handleEncrypted(context.Background(), DecodedFrame{
		Encrypted: &mtproto.InnerData{
			Salt: inboundSalt, SessionID: inboundSessionID,
			MsgID: inboundMessageID(4), SeqNo: 1,
			Data: encodeControlBody(t, &mtprototl.InvokeWithoutUpdates{QueryRaw: constructorBody(bindConstructor)}),
		},
		AuthKeyID: first.authKey.ID(), AuthKey: first.authKey,
	}); err != nil {
		t.Fatalf("initial cold request: %v", err)
	}
	waitForWrittenFrames(t, first.transport, 3)
	snapshot := loadConnectionSessionSnapshot(t, store, first.authKey.ID(), inboundSessionID)
	if snapshot.PushSubscription {
		t.Fatalf("cold session unexpectedly persisted push subscription: %+v", snapshot)
	}
	first.connection.shutdown(io.EOF)

	second := newConnectionHarnessWithStore(t, now, application, 100, nil, store)
	second.connection.config.Presence = presence
	defer second.connection.shutdown(io.EOF)
	actor, err := second.connection.sessionFor(context.Background(), DecodedFrame{
		Encrypted: &mtproto.InnerData{Salt: inboundSalt, SessionID: inboundSessionID},
		AuthKeyID: second.authKey.ID(), AuthKey: second.authKey,
	})
	if err != nil {
		t.Fatalf("cold reconnect session: %v", err)
	}
	if actor.acceptsPush {
		t.Fatal("cold reconnect restored an active push subscription")
	}
	if err := presence.publish(context.Background(), 42, constructorBody(0x82828282)); err == nil {
		t.Fatal("cold reconnect unexpectedly accepted a server push")
	}
}

func TestConnectionRPCDropCancelsRunningGeneratedHandler(t *testing.T) {
	now := time.Unix(inboundNowSeconds, 0).UTC()
	requestID := inboundMessageID(4)
	dropID := inboundMessageID(8)
	application := &connectionApplicationStub{block: true, started: make(chan struct{})}
	dropBody := encodeControlBody(t, &mtprototl.RPCDropAnswer{ReqMsgID: requestID})
	harness := newConnectionHarness(t, now, application, 3, []mtproto.InnerData{
		{Salt: inboundSalt, SessionID: inboundSessionID, MsgID: requestID, SeqNo: 1, Data: constructorBody(0x30303030)},
		{Salt: inboundSalt, SessionID: inboundSessionID, MsgID: dropID, SeqNo: 3, Data: dropBody},
	})
	harness.connection.config.ActiveRequests = 1
	harness.connection.admission = newConnectionRequestAdmission(1)

	err := harness.connection.Run(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("Run() error = %v, want EOF", err)
	}
	select {
	case <-application.canceled:
	default:
		t.Fatal("rpc_drop_answer did not cancel the running generated handler")
	}
	frames := harness.transport.writtenFrames()
	if len(frames) != 3 {
		t.Fatalf("written frames = %d, want new_session_created + drop result + ack", len(frames))
	}
	resultInner := decryptWriterFrame(t, harness.authKey, frames[1])
	result := &mtprototl.RPCResult{}
	if err := decodeControl(resultInner.Data, result); err != nil {
		t.Fatal(err)
	}
	if result.ReqMsgID != dropID || binaryConstructor(result.ResultRaw) != mtprototl.RPCAnswerDroppedRunningID {
		t.Fatalf("drop result = %+v", result)
	}
}

type connectionHarness struct {
	connection *Connection
	transport  *scriptedFrameConnection
	authKey    crypto.AuthKey
	store      *session.MemoryStore
}

func newConnectionHarness(t *testing.T, now time.Time, application ApplicationDispatcher, writes int, messages []mtproto.InnerData) connectionHarness {
	return newConnectionHarnessWithStore(t, now, application, writes, messages, session.NewMemoryStore())
}

func newConnectionHarnessWithStore(t *testing.T, now time.Time, application ApplicationDispatcher, writes int, messages []mtproto.InnerData, store *session.MemoryStore) connectionHarness {
	t.Helper()
	var authKey crypto.AuthKey
	for index := range authKey {
		authKey[index] = byte(index + 1)
	}
	authKeys := crypto.NewMemoryAuthKeyManager()
	if err := authKeys.Put(authKey.ID(), authKey); err != nil {
		t.Fatal(err)
	}
	frames := make([][]byte, 0, len(messages))
	for index := range messages {
		encrypted, err := messages[index].EncryptFromClient(authKey, authKey.ID())
		if err != nil {
			t.Fatalf("encrypt request: %v", err)
		}
		frames = append(frames, serializeEncryptedFrame(encrypted))
	}
	transport := newScriptedFrameConnection(frames, writes)
	reliabilityRegistry, err := NewReliabilityRegistry(ReliabilityRegistryConfig{
		MaxSessions: 4, MessageCapacity: 32, TTL: time.Minute,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	handshakeEngine, err := handshake.New(handshake.Config{
		AuthKeys: authKeys, ServerKeys: crypto.NewMemoryServerKeyManager(),
	})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := NewConnection(ConnectionConfig{
		Conn: transport, AuthKeys: authKeys, Handshake: handshakeEngine,
		Leases: NewSessionLeaseRegistry(store), Reliability: reliabilityRegistry,
		Application: application, MessageIDs: &fixedMessageIDs{next: now.Unix()<<32 | 100},
		Transport: "test", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return connectionHarness{connection: connection, transport: transport, authKey: authKey, store: store}
}

type connectionApplicationStub struct {
	mu       sync.Mutex
	outcome  Outcome
	pushBody []byte
	request  Request
	block    bool
	started  chan struct{}
	canceled chan struct{}
}

type subscriptionApplicationStub struct {
	bindConstructor       uint32
	suppressedConstructor uint32
	incidentalSenderPush  uint32
	incidentalOutcomePush uint32
}

func (s *subscriptionApplicationStub) DispatchApplication(ctx context.Context, request Request) (Outcome, error) {
	response := RPCResult{RequestMessageID: request.Message.MessageID, Body: constructorBody(0x51515151)}
	switch request.Message.ConstructorID {
	case s.bindConstructor:
		return Outcome{Intents: []Intent{response}, Mutations: []SessionMutation{BindUser{UserID: 42}}}, nil
	case s.suppressedConstructor:
		if err := request.Info.Sender.Push(ctx, constructorBody(s.incidentalSenderPush)); err != nil {
			return Outcome{}, err
		}
		return Outcome{Intents: []Intent{
			Push{Body: constructorBody(s.incidentalOutcomePush)},
			response,
		}}, nil
	default:
		return Outcome{Intents: []Intent{response}}, nil
	}
}

type subscriptionPresenceStub struct {
	mu      sync.Mutex
	byUser  map[int64]Sender
	updated chan struct{}
}

func newSubscriptionPresenceStub() *subscriptionPresenceStub {
	return &subscriptionPresenceStub{byUser: make(map[int64]Sender), updated: make(chan struct{}, 8)}
}

func (s *subscriptionPresenceStub) Update(snapshot session.Snapshot, sender Sender, acceptsPush bool) {
	s.mu.Lock()
	if snapshot.UserID != 0 {
		if acceptsPush {
			s.byUser[snapshot.UserID] = sender
		} else if s.byUser[snapshot.UserID] == sender {
			delete(s.byUser, snapshot.UserID)
		}
	}
	s.mu.Unlock()
	select {
	case s.updated <- struct{}{}:
	default:
	}
}

func (s *subscriptionPresenceStub) Remove(_ session.SessionKey, sender Sender) {
	s.mu.Lock()
	for userID, current := range s.byUser {
		if current == sender {
			delete(s.byUser, userID)
		}
	}
	s.mu.Unlock()
}

func (s *subscriptionPresenceStub) publish(ctx context.Context, userID int64, body []byte) error {
	s.mu.Lock()
	sender := s.byUser[userID]
	s.mu.Unlock()
	if sender == nil {
		return errors.New("bound session is not subscribed for server push")
	}
	return sender.Push(ctx, body)
}

func (s *subscriptionPresenceStub) waitForUser(t *testing.T, userID int64) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		s.mu.Lock()
		bound := s.byUser[userID] != nil
		s.mu.Unlock()
		if bound {
			return
		}
		select {
		case <-s.updated:
		case <-deadline.C:
			t.Fatalf("user %d was not bound for push", userID)
		}
	}
}

func waitForWrittenFrames(t *testing.T, transport *scriptedFrameConnection, count int) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if len(transport.writtenFrames()) >= count {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("written frames = %d, want at least %d", len(transport.writtenFrames()), count)
		}
	}
}

func containsConstructor(constructors []uint32, want uint32) bool {
	for _, constructor := range constructors {
		if constructor == want {
			return true
		}
	}
	return false
}

func (s *connectionApplicationStub) DispatchApplication(ctx context.Context, request Request) (Outcome, error) {
	s.mu.Lock()
	s.request = request
	block := s.block
	started := s.started
	if s.canceled == nil {
		s.canceled = make(chan struct{})
	}
	canceled := s.canceled
	outcome := s.outcome
	pushBody := append([]byte(nil), s.pushBody...)
	s.mu.Unlock()
	if !block {
		if len(pushBody) != 0 {
			if err := request.Info.Sender.Push(ctx, pushBody); err != nil {
				return Outcome{}, err
			}
		}
		return outcome, nil
	}
	if started != nil {
		close(started)
	}
	<-ctx.Done()
	close(canceled)
	return Outcome{}, ctx.Err()
}

type scriptedFrameConnection struct {
	mu          sync.Mutex
	frames      [][]byte
	writes      [][]byte
	wantWrites  int
	release     chan struct{}
	releaseOnce sync.Once
	ctx         context.Context
	cancel      context.CancelFunc
}

func newScriptedFrameConnection(frames [][]byte, wantWrites int) *scriptedFrameConnection {
	ctx, cancel := context.WithCancel(context.Background())
	return &scriptedFrameConnection{
		frames: frames, wantWrites: wantWrites,
		release: make(chan struct{}), ctx: ctx, cancel: cancel,
	}
}

func (c *scriptedFrameConnection) ReadMessage(int) ([]byte, error) {
	c.mu.Lock()
	if len(c.frames) != 0 {
		frame := append([]byte(nil), c.frames[0]...)
		c.frames = c.frames[1:]
		c.mu.Unlock()
		return frame, nil
	}
	c.mu.Unlock()
	<-c.release
	return nil, io.EOF
}

func (c *scriptedFrameConnection) WriteMessage(frame []byte) error {
	c.mu.Lock()
	c.writes = append(c.writes, append([]byte(nil), frame...))
	reached := len(c.writes) >= c.wantWrites
	c.mu.Unlock()
	if reached {
		c.releaseOnce.Do(func() { close(c.release) })
	}
	return nil
}

func (*scriptedFrameConnection) SetWriteDeadline(time.Time) error { return nil }

func (c *scriptedFrameConnection) Close() error {
	c.cancel()
	c.releaseOnce.Do(func() { close(c.release) })
	return nil
}

func (c *scriptedFrameConnection) Context() context.Context { return c.ctx }
func (c *scriptedFrameConnection) LocalAddr() net.Addr      { return testNetworkAddress("local") }
func (c *scriptedFrameConnection) RemoteAddr() net.Addr     { return testNetworkAddress("remote") }

func (c *scriptedFrameConnection) writtenFrames() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([][]byte, len(c.writes))
	for index := range c.writes {
		result[index] = append([]byte(nil), c.writes[index]...)
	}
	return result
}

type testNetworkAddress string

func (a testNetworkAddress) Network() string { return "test" }
func (a testNetworkAddress) String() string  { return string(a) }

var _ FrameConnection = (*scriptedFrameConnection)(nil)
