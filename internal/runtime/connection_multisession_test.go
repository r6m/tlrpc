package runtime

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/session"
)

func TestConnectionRoutesAlternatingSessionsOnOnePhysicalConnection(t *testing.T) {
	now := time.Unix(inboundNowSeconds, 0).UTC()
	application := &multisessionApplicationStub{}
	harness := newConnectionHarness(t, now, application, 100, nil)
	defer harness.connection.shutdown(io.EOF)

	const sessionB = int64(0x2233445566778899)
	requests := []struct {
		sessionID int64
		messageID int64
		sequence  int32
	}{
		{sessionID: inboundSessionID, messageID: inboundMessageID(4), sequence: 1},
		{sessionID: sessionB, messageID: inboundMessageID(8), sequence: 1},
		{sessionID: inboundSessionID, messageID: inboundMessageID(12), sequence: 3},
	}
	for index, request := range requests {
		if err := harness.connection.handleEncrypted(context.Background(), DecodedFrame{
			Encrypted: &mtproto.InnerData{
				Salt:      inboundSalt,
				SessionID: request.sessionID,
				MsgID:     request.messageID,
				SeqNo:     request.sequence,
				Data:      constructorBody(0x71717171),
			},
			AuthKeyID: harness.authKey.ID(),
			AuthKey:   harness.authKey,
		}); err != nil {
			t.Fatalf("handle request %d: %v", index, err)
		}
		waitForWrittenFrames(t, harness.transport, []int{3, 6, 8}[index])
	}

	framesBySession := make(map[int64]int)
	for _, frame := range harness.transport.writtenFrames() {
		inner := decryptWriterFrame(t, harness.authKey, frame)
		framesBySession[inner.SessionID]++
	}
	if framesBySession[inboundSessionID] != 5 || framesBySession[sessionB] != 3 {
		t.Fatalf("frames by session = %v, want A=5 B=3", framesBySession)
	}
	if got := application.sessionIDs(); !equalInt64s(got, []int64{inboundSessionID, sessionB, inboundSessionID}) {
		t.Fatalf("application session order = %v", got)
	}

	snapshotA := loadConnectionSessionSnapshot(t, harness.store, harness.authKey.ID(), inboundSessionID)
	snapshotB := loadConnectionSessionSnapshot(t, harness.store, harness.authKey.ID(), sessionB)
	if snapshotA.SeqNo != 4 || snapshotA.ServerSeqNo != 2 {
		t.Fatalf("session A progress = client %d server %d, want 4/2", snapshotA.SeqNo, snapshotA.ServerSeqNo)
	}
	if snapshotB.SeqNo != 2 || snapshotB.ServerSeqNo != 1 {
		t.Fatalf("session B progress = client %d server %d, want 2/1", snapshotB.SeqNo, snapshotB.ServerSeqNo)
	}
}

func TestConnectionAdmitsTwoInitialSameAuthSessionsWithoutRetiringEither(t *testing.T) {
	now := time.Unix(inboundNowSeconds, 0).UTC()
	application := &concurrentSessionsApplicationStub{
		started: make(chan int64, 2),
		release: make(chan struct{}),
	}
	harness := newConnectionHarness(t, now, application, 100, nil)
	defer harness.connection.shutdown(io.EOF)

	const sessionB = int64(0x1122334455667788)
	for index, sessionID := range []int64{inboundSessionID, sessionB} {
		if err := harness.connection.handleEncrypted(context.Background(), DecodedFrame{
			Encrypted: &mtproto.InnerData{
				Salt: inboundSalt, SessionID: sessionID,
				MsgID: inboundMessageID(uint32(4 + index*4)), SeqNo: 1,
				Data: constructorBody(0x70707070),
			},
			AuthKeyID: harness.authKey.ID(), AuthKey: harness.authKey,
		}); err != nil {
			t.Fatalf("initial request for session %d: %v", sessionID, err)
		}
		select {
		case startedSession := <-application.started:
			if startedSession != sessionID {
				t.Fatalf("started session = %d, want %d", startedSession, sessionID)
			}
		case <-time.After(time.Second):
			t.Fatalf("session %d handler did not start", sessionID)
		}
	}

	harness.connection.mu.Lock()
	actors := make([]*connectionSession, 0, len(harness.connection.sessions))
	for _, actor := range harness.connection.sessions {
		actors = append(actors, actor)
	}
	harness.connection.mu.Unlock()
	if len(actors) != 2 {
		t.Fatalf("attached sessions = %d, want 2", len(actors))
	}
	for _, actor := range actors {
		if err := context.Cause(actor.lease.Context()); err != nil {
			t.Fatalf("session %d was retired during parallel initial activity: %v", actor.key.SessionID, err)
		}
	}

	close(application.release)
	waitForWrittenFrames(t, harness.transport, 6)
	for _, actor := range actors {
		if err := context.Cause(actor.lease.Context()); err != nil {
			t.Fatalf("session %d retired after its initial request: %v", actor.key.SessionID, err)
		}
	}
}

func TestConnectionAcceptsHighInitialSequenceForClientRestoredSession(t *testing.T) {
	now := time.Unix(inboundNowSeconds, 0).UTC()
	application := &multisessionApplicationStub{}
	harness := newConnectionHarness(t, now, application, 100, nil)
	defer harness.connection.shutdown(io.EOF)

	const restoredSequence = int32(501)
	if err := harness.connection.handleEncrypted(context.Background(), DecodedFrame{
		Encrypted: &mtproto.InnerData{
			Salt: inboundSalt, SessionID: inboundSessionID,
			MsgID: inboundMessageID(4), SeqNo: restoredSequence,
			Data: constructorBody(0x70707070),
		},
		AuthKeyID: harness.authKey.ID(), AuthKey: harness.authKey,
	}); err != nil {
		t.Fatalf("restored-session request: %v", err)
	}
	waitForWrittenFrames(t, harness.transport, 3)
	if got := application.sessionIDs(); !equalInt64s(got, []int64{inboundSessionID}) {
		t.Fatalf("application sessions = %v, want restored session", got)
	}
	snapshot := loadConnectionSessionSnapshot(t, harness.store, harness.authKey.ID(), inboundSessionID)
	if snapshot.SeqNo != restoredSequence+1 {
		t.Fatalf("restored client sequence = %d, want %d", snapshot.SeqNo, restoredSequence+1)
	}
}

func TestConnectionPinsPhysicalTransportToOneAuthKey(t *testing.T) {
	now := time.Unix(inboundNowSeconds, 0).UTC()
	harness := newConnectionHarness(t, now, &multisessionApplicationStub{}, 100, nil)
	defer harness.connection.shutdown(io.EOF)

	first := DecodedFrame{
		Encrypted: &mtproto.InnerData{Salt: inboundSalt, SessionID: inboundSessionID},
		AuthKeyID: harness.authKey.ID(),
		AuthKey:   harness.authKey,
	}
	if _, err := harness.connection.sessionFor(context.Background(), first); err != nil {
		t.Fatalf("bind first auth key: %v", err)
	}
	other := harness.authKey
	other[0] ^= 0xff
	second := DecodedFrame{
		Encrypted: &mtproto.InnerData{Salt: inboundSalt, SessionID: inboundSessionID + 1},
		AuthKeyID: other.ID(),
		AuthKey:   other,
	}
	if _, err := harness.connection.sessionFor(context.Background(), second); !errors.Is(err, ErrConnectionProtocol) {
		t.Fatalf("second auth key error = %v, want ErrConnectionProtocol", err)
	}
}

func TestConnectionSessionMapIsBounded(t *testing.T) {
	now := time.Unix(inboundNowSeconds, 0).UTC()
	harness := newConnectionHarness(t, now, &multisessionApplicationStub{}, 100, nil)
	harness.connection.config.SessionCapacity = 2
	defer harness.connection.shutdown(io.EOF)

	for index, sessionID := range []int64{inboundSessionID, inboundSessionID + 1, inboundSessionID + 2} {
		_, err := harness.connection.sessionFor(context.Background(), DecodedFrame{
			Encrypted: &mtproto.InnerData{Salt: inboundSalt, SessionID: sessionID},
			AuthKeyID: harness.authKey.ID(),
			AuthKey:   harness.authKey,
		})
		if index < 2 && err != nil {
			t.Fatalf("admit session %d: %v", index, err)
		}
		if index == 2 {
			var capacityErr *ConnectionSessionCapacityError
			if !errors.As(err, &capacityErr) || capacityErr.Capacity != 2 {
				t.Fatalf("third session error = %v, want capacity 2", err)
			}
		}
	}
	harness.connection.mu.Lock()
	count := len(harness.connection.sessions)
	harness.connection.mu.Unlock()
	if count != 2 {
		t.Fatalf("attached sessions = %d, want 2", count)
	}
}

func TestConnectionInFlightLimitIsSharedAcrossSessions(t *testing.T) {
	now := time.Unix(inboundNowSeconds, 0).UTC()
	application := &connectionApplicationStub{block: true, started: make(chan struct{})}
	harness := newConnectionHarness(t, now, application, 100, nil)
	harness.connection.config.ActiveRequests = 1
	harness.connection.admission = newConnectionRequestAdmission(1)
	defer harness.connection.shutdown(io.EOF)

	if err := harness.connection.handleEncrypted(context.Background(), DecodedFrame{
		Encrypted: &mtproto.InnerData{
			Salt: inboundSalt, SessionID: inboundSessionID,
			MsgID: inboundMessageID(4), SeqNo: 1, Data: constructorBody(0x72727272),
		},
		AuthKeyID: harness.authKey.ID(), AuthKey: harness.authKey,
	}); err != nil {
		t.Fatalf("first session request: %v", err)
	}
	select {
	case <-application.started:
	case <-time.After(time.Second):
		t.Fatal("first session handler did not start")
	}

	const sessionB = int64(0x3344556677889900)
	err := harness.connection.handleEncrypted(context.Background(), DecodedFrame{
		Encrypted: &mtproto.InnerData{
			Salt: inboundSalt, SessionID: sessionB,
			MsgID: inboundMessageID(8), SeqNo: 1, Data: constructorBody(0x73737373),
		},
		AuthKeyID: harness.authKey.ID(), AuthKey: harness.authKey,
	})
	var capacityErr *ActiveRequestCapacityError
	if !errors.As(err, &capacityErr) || capacityErr.Capacity != 1 {
		t.Fatalf("second session request error = %v, want shared capacity 1", err)
	}
}

func TestConnectionLeaseReplacementRetiresOnlyMatchingSession(t *testing.T) {
	now := time.Unix(inboundNowSeconds, 0).UTC()
	application := &multisessionApplicationStub{}
	first := newConnectionHarness(t, now, application, 100, nil)

	const sessionB = int64(0x4455667788990011)
	for _, sessionID := range []int64{inboundSessionID, sessionB} {
		if _, err := first.connection.sessionFor(context.Background(), DecodedFrame{
			Encrypted: &mtproto.InnerData{Salt: inboundSalt, SessionID: sessionID},
			AuthKeyID: first.authKey.ID(), AuthKey: first.authKey,
		}); err != nil {
			t.Fatalf("attach first connection session %d: %v", sessionID, err)
		}
	}

	secondTransport := newScriptedFrameConnection(nil, 100)
	second, err := NewConnection(ConnectionConfig{
		Conn:              secondTransport,
		AuthKeys:          first.connection.config.AuthKeys,
		Handshake:         first.connection.config.Handshake,
		Leases:            first.connection.config.Leases,
		Reliability:       first.connection.config.Reliability,
		Application:       application,
		MessageIDs:        &fixedMessageIDs{next: now.Unix()<<32 | 1000},
		MaxDecodedPayload: first.connection.config.MaxDecodedPayload,
		Transport:         "test",
		Now:               func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer first.connection.shutdown(io.EOF)
	defer second.shutdown(io.EOF)

	if _, err := second.sessionFor(context.Background(), DecodedFrame{
		Encrypted: &mtproto.InnerData{Salt: inboundSalt, SessionID: sessionB},
		AuthKeyID: first.authKey.ID(), AuthKey: first.authKey,
	}); err != nil {
		t.Fatalf("replace session B lease: %v", err)
	}
	waitForConnectionSessionCount(t, first.connection, 1)
	first.connection.mu.Lock()
	actorA := first.connection.sessions[session.SessionKey{AuthKeyID: first.authKey.ID(), SessionID: inboundSessionID}]
	actorB := first.connection.sessions[session.SessionKey{AuthKeyID: first.authKey.ID(), SessionID: sessionB}]
	first.connection.mu.Unlock()
	if actorA == nil || actorB != nil {
		t.Fatalf("first connection sessions after replacement: A=%v B=%v", actorA != nil, actorB != nil)
	}
	if err := first.transport.Context().Err(); err != nil {
		t.Fatalf("replacing session B closed shared physical transport: %v", err)
	}

	if err := first.connection.handleEncrypted(context.Background(), DecodedFrame{
		Encrypted: &mtproto.InnerData{
			Salt: inboundSalt, SessionID: inboundSessionID,
			MsgID: inboundMessageID(4), SeqNo: 1, Data: constructorBody(0x75757575),
		},
		AuthKeyID: first.authKey.ID(), AuthKey: first.authKey,
	}); err != nil {
		t.Fatalf("session A after replacing B: %v", err)
	}
	waitForWrittenFrames(t, first.transport, 3)
}

type multisessionApplicationStub struct {
	mu       sync.Mutex
	sessions []int64
}

type concurrentSessionsApplicationStub struct {
	started chan int64
	release chan struct{}
}

func (s *concurrentSessionsApplicationStub) DispatchApplication(ctx context.Context, request Request) (Outcome, error) {
	select {
	case s.started <- request.Info.SessionID:
	case <-ctx.Done():
		return Outcome{}, ctx.Err()
	}
	select {
	case <-s.release:
		return Outcome{Intents: []Intent{RPCResult{
			RequestMessageID: request.Message.MessageID,
			Body:             constructorBody(0x76767676),
		}}}, nil
	case <-ctx.Done():
		return Outcome{}, ctx.Err()
	}
}

func (s *multisessionApplicationStub) DispatchApplication(_ context.Context, request Request) (Outcome, error) {
	s.mu.Lock()
	s.sessions = append(s.sessions, request.Info.SessionID)
	s.mu.Unlock()
	return Outcome{Intents: []Intent{RPCResult{
		RequestMessageID: request.Message.MessageID,
		Body:             constructorBody(0x74747474),
	}}}, nil
}

func (s *multisessionApplicationStub) sessionIDs() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.sessions...)
}

func loadConnectionSessionSnapshot(t *testing.T, store *session.MemoryStore, authKeyID crypto.KeyID, sessionID int64) session.Snapshot {
	t.Helper()
	snapshot, err := store.Load(context.Background(), session.SessionKey{AuthKeyID: authKeyID, SessionID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func equalInt64s(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func waitForConnectionSessionCount(t *testing.T, connection *Connection, want int) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		connection.mu.Lock()
		got := len(connection.sessions)
		connection.mu.Unlock()
		if got == want {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("attached sessions = %d, want %d", got, want)
		}
	}
}
