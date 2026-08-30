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
	store := session.NewMemoryStore()
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
