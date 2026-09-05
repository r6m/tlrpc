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
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
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

func TestConnectionAdmissionSaturationReturnsCorrelatedRetryableErrorAcrossSessions(t *testing.T) {
	now := time.Unix(inboundNowSeconds, 0).UTC()
	application := &concurrentSessionsApplicationStub{started: make(chan int64, 2), release: make(chan struct{})}
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
	case sessionID := <-application.started:
		if sessionID != inboundSessionID {
			t.Fatalf("started session = %d, want %d", sessionID, inboundSessionID)
		}
	case <-time.After(time.Second):
		t.Fatal("first session handler did not start")
	}

	const sessionB = int64(0x3344556677889900)
	secondFrame := DecodedFrame{
		Encrypted: &mtproto.InnerData{
			Salt: inboundSalt, SessionID: sessionB,
			MsgID: inboundMessageID(8), SeqNo: 1, Data: constructorBody(0x73737373),
		},
		AuthKeyID: harness.authKey.ID(), AuthKey: harness.authKey,
	}
	if err := harness.connection.handleEncrypted(context.Background(), secondFrame); err != nil {
		t.Fatalf("saturated request: %v", err)
	}
	waitForWrittenFrames(t, harness.transport, 2)
	foundOverload := false
	for _, frame := range harness.transport.writtenFrames() {
		inner := decryptWriterFrame(t, harness.authKey, frame)
		if inner.SessionID != sessionB || binaryConstructor(inner.Data) != mtprototl.RPCResultID {
			continue
		}
		result := &mtprototl.RPCResult{}
		if err := decodeControl(inner.Data, result); err != nil {
			t.Fatal(err)
		}
		rpcErr := &mtprototl.RPCError{}
		if err := decodeControl(result.ResultRaw, rpcErr); err != nil {
			t.Fatal(err)
		}
		foundOverload = result.ReqMsgID == secondFrame.Encrypted.MsgID && rpcErr.ErrorCode == 500 && rpcErr.ErrorMessage == "SERVER_BUSY"
	}
	if !foundOverload {
		t.Fatal("saturated request did not receive correlated SERVER_BUSY")
	}
	snapshot := loadConnectionSessionSnapshot(t, harness.store, harness.authKey.ID(), sessionB)
	if snapshot.SeqNo != 0 || snapshot.LastClientMsgID != 0 || len(snapshot.RecentClientMsgIDs) != 0 {
		t.Fatalf("saturated request consumed durable inbound state: %+v", snapshot)
	}

	close(application.release)
	waitForWrittenFrames(t, harness.transport, 4)
	if err := harness.connection.handleEncrypted(context.Background(), secondFrame); err != nil {
		t.Fatalf("retry same request: %v", err)
	}
	select {
	case sessionID := <-application.started:
		if sessionID != sessionB {
			t.Fatalf("retried session = %d, want %d", sessionID, sessionB)
		}
	case <-time.After(time.Second):
		t.Fatal("retryable saturated request did not execute")
	}
}

func TestConnectionContainerAdmissionIsAtomicAndCorrelated(t *testing.T) {
	now := time.Unix(inboundNowSeconds, 0).UTC()
	application := &multisessionApplicationStub{}
	harness := newConnectionHarness(t, now, application, 100, nil)
	harness.connection.config.ActiveRequests = 1
	harness.connection.admission = newConnectionRequestAdmission(1)
	defer harness.connection.shutdown(io.EOF)

	firstID := inboundMessageID(4)
	secondID := inboundMessageID(8)
	body := serializeInboundContainer(t, []mtprototl.Message{
		{MsgID: firstID, SeqNo: 1, BodyRaw: constructorBody(0x74747474)},
		{MsgID: secondID, SeqNo: 3, BodyRaw: constructorBody(0x75757575)},
	})
	if err := harness.connection.handleEncrypted(context.Background(), DecodedFrame{
		Encrypted: &mtproto.InnerData{
			Salt: inboundSalt, SessionID: inboundSessionID,
			MsgID: inboundMessageID(20), SeqNo: 4, Data: body,
		},
		AuthKeyID: harness.authKey.ID(), AuthKey: harness.authKey,
	}); err != nil {
		t.Fatalf("saturated container: %v", err)
	}
	waitForWrittenFrames(t, harness.transport, 1)
	if got := application.sessionIDs(); len(got) != 0 {
		t.Fatalf("container executed a prefix: %v", got)
	}
	snapshot := loadConnectionSessionSnapshot(t, harness.store, harness.authKey.ID(), inboundSessionID)
	if snapshot.SeqNo != 0 || snapshot.LastClientMsgID != 0 || len(snapshot.RecentClientMsgIDs) != 0 {
		t.Fatalf("saturated container consumed durable inbound state: %+v", snapshot)
	}

	frames := harness.transport.writtenFrames()
	inner := decryptWriterFrame(t, harness.authKey, frames[0])
	container := &mtprototl.MsgContainer{}
	if err := decodeControl(inner.Data, container); err != nil {
		t.Fatalf("decode overload container: %v", err)
	}
	if len(container.Messages) != 2 {
		t.Fatalf("overload result count = %d, want 2", len(container.Messages))
	}
	wantIDs := []int64{firstID, secondID}
	for index, child := range container.Messages {
		result := &mtprototl.RPCResult{}
		if err := decodeControl(child.BodyRaw, result); err != nil {
			t.Fatalf("decode result %d: %v", index, err)
		}
		rpcErr := &mtprototl.RPCError{}
		if err := decodeControl(result.ResultRaw, rpcErr); err != nil {
			t.Fatalf("decode rpc error %d: %v", index, err)
		}
		if result.ReqMsgID != wantIDs[index] || rpcErr.ErrorCode != 500 || rpcErr.ErrorMessage != "SERVER_BUSY" {
			t.Fatalf("overload result %d = req %d error %+v", index, result.ReqMsgID, rpcErr)
		}
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
		Sessions:          first.connection.config.Sessions,
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
	if _, err := first.connection.sessionFor(context.Background(), DecodedFrame{
		Encrypted: &mtproto.InnerData{Salt: inboundSalt, SessionID: sessionB},
		AuthKeyID: first.authKey.ID(), AuthKey: first.authKey,
	}); !errors.Is(err, session.ErrLeaseLost) {
		t.Fatalf("stale connection reacquire error = %v, want ErrLeaseLost", err)
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

func TestConnectionReloadHandoverCompletesOrClosesConcurrentStartupRPCs(t *testing.T) {
	const startupRequests = 5
	for _, tc := range []struct {
		name         string
		reuseSession bool
	}{
		{name: "new session overlaps old connection", reuseSession: false},
		{name: "same session lease moves to replacement connection", reuseSession: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(inboundNowSeconds, 0).UTC()
			application := newReloadHandoverApplicationStub(startupRequests)
			first := newConnectionHarness(t, now, application, 100, nil)
			first.connection.config.ConnectionID = 101
			defer first.connection.shutdown(io.EOF)

			for index := 0; index < startupRequests; index++ {
				requestID := inboundMessageID(uint32(4 + index*4))
				if err := first.connection.handleEncrypted(context.Background(), DecodedFrame{
					Encrypted: &mtproto.InnerData{
						Salt: inboundSalt, SessionID: inboundSessionID,
						MsgID: requestID, SeqNo: int32(index*2 + 1), Data: constructorBody(0x78787878),
					},
					AuthKeyID: first.authKey.ID(), AuthKey: first.authKey,
				}); err != nil {
					t.Fatalf("admit old request %d: %v", requestID, err)
				}
				application.waitStarted(t, 101, requestID)
			}

			secondTransport := newScriptedFrameConnection(nil, 100)
			second, err := NewConnection(ConnectionConfig{
				ConnectionID:      202,
				Conn:              secondTransport,
				AuthKeys:          first.connection.config.AuthKeys,
				Handshake:         first.connection.config.Handshake,
				Sessions:          first.connection.config.Sessions,
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
			defer second.shutdown(io.EOF)

			secondSessionID := inboundSessionID + 1
			firstSequence := int32(1)
			requestLow := uint32(100)
			if tc.reuseSession {
				secondSessionID = inboundSessionID
				firstSequence = startupRequests*2 + 1
			}
			requestIDs := make([]int64, startupRequests)
			firstResult := make(chan error, 1)
			requestIDs[0] = inboundMessageID(requestLow)
			go func() {
				firstResult <- second.handleEncrypted(context.Background(), DecodedFrame{
					Encrypted: &mtproto.InnerData{
						Salt: inboundSalt, SessionID: secondSessionID,
						MsgID: requestIDs[0], SeqNo: firstSequence, Data: constructorBody(0x79797979),
					},
					AuthKeyID: first.authKey.ID(), AuthKey: first.authKey,
				})
			}()
			application.waitStarted(t, 202, requestIDs[0])
			if err := <-firstResult; err != nil {
				t.Fatalf("admit first replacement request: %v", err)
			}

			for index := 1; index < startupRequests; index++ {
				requestIDs[index] = inboundMessageID(requestLow + uint32(index*4))
				if err := second.handleEncrypted(context.Background(), DecodedFrame{
					Encrypted: &mtproto.InnerData{
						Salt: inboundSalt, SessionID: secondSessionID,
						MsgID: requestIDs[index], SeqNo: firstSequence + int32(index*2), Data: constructorBody(0x79797979),
					},
					AuthKeyID: first.authKey.ID(), AuthKey: first.authKey,
				}); err != nil {
					t.Fatalf("admit replacement request %d: %v", requestIDs[index], err)
				}
				application.waitStarted(t, 202, requestIDs[index])
			}

			if !tc.reuseSession {
				first.connection.shutdown(io.EOF)
			}
			application.waitCanceled(t, startupRequests)
			close(application.releaseReplacement)

			wantFrames := startupRequests * 2
			if !tc.reuseSession {
				wantFrames++ // new_session_created for the fresh session
			}
			waitForWrittenFrames(t, secondTransport, wantFrames)
			assertCorrelatedStartupResponses(t, secondTransport.writtenFrames(), first.authKey, secondSessionID, requestIDs)
			if tc.reuseSession && first.transport.Context().Err() == nil {
				t.Fatal("displaced transport stayed open after its last session lease was lost")
			}
		})
	}
}

type reloadHandoverApplicationStub struct {
	started            chan reloadHandoverRequest
	canceled           chan struct{}
	releaseReplacement chan struct{}
}

type reloadHandoverRequest struct {
	connectionID uint64
	messageID    int64
}

func newReloadHandoverApplicationStub(capacity int) *reloadHandoverApplicationStub {
	return &reloadHandoverApplicationStub{
		started:            make(chan reloadHandoverRequest, capacity*2),
		canceled:           make(chan struct{}, capacity),
		releaseReplacement: make(chan struct{}),
	}
}

func (s *reloadHandoverApplicationStub) DispatchApplication(ctx context.Context, request Request) (Outcome, error) {
	started := reloadHandoverRequest{connectionID: request.Info.ConnectionID, messageID: request.Message.MessageID}
	select {
	case s.started <- started:
	case <-ctx.Done():
		return Outcome{}, ctx.Err()
	}
	if request.Info.ConnectionID == 101 {
		<-ctx.Done()
		s.canceled <- struct{}{}
		return Outcome{}, ctx.Err()
	}
	select {
	case <-s.releaseReplacement:
		return Outcome{Intents: []Intent{RPCResult{
			RequestMessageID: request.Message.MessageID,
			Body:             constructorBody(0x7a7a7a7a),
		}}}, nil
	case <-ctx.Done():
		return Outcome{}, ctx.Err()
	}
}

func (s *reloadHandoverApplicationStub) waitStarted(t *testing.T, connectionID uint64, messageID int64) {
	t.Helper()
	select {
	case got := <-s.started:
		if got.connectionID != connectionID || got.messageID != messageID {
			t.Fatalf("started request = %+v, want connection=%d message=%d", got, connectionID, messageID)
		}
	case <-time.After(time.Second):
		t.Fatalf("connection %d request %d did not start", connectionID, messageID)
	}
}

func (s *reloadHandoverApplicationStub) waitCanceled(t *testing.T, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		select {
		case <-s.canceled:
		case <-time.After(time.Second):
			t.Fatalf("old request cancellations = %d, want %d", index, count)
		}
	}
}

func assertCorrelatedStartupResponses(t *testing.T, frames [][]byte, authKey crypto.AuthKey, sessionID int64, requestIDs []int64) {
	t.Helper()
	results := make(map[int64]int, len(requestIDs))
	acknowledged := make(map[int64]int, len(requestIDs))
	for _, frame := range frames {
		inner := decryptWriterFrame(t, authKey, frame)
		if inner.SessionID != sessionID {
			t.Fatalf("response session = %d, want %d", inner.SessionID, sessionID)
		}
		switch binaryConstructor(inner.Data) {
		case mtprototl.RPCResultID:
			result := &mtprototl.RPCResult{}
			if err := decodeControl(inner.Data, result); err != nil {
				t.Fatalf("decode rpc_result: %v", err)
			}
			if binaryConstructor(result.ResultRaw) != 0x7a7a7a7a {
				t.Fatalf("request %d result constructor = %08x", result.ReqMsgID, binaryConstructor(result.ResultRaw))
			}
			results[result.ReqMsgID]++
		case mtprototl.MsgsAckID:
			ack := &mtprototl.MsgsAck{}
			if err := decodeControl(inner.Data, ack); err != nil {
				t.Fatalf("decode msgs_ack: %v", err)
			}
			for _, messageID := range ack.MsgIDs {
				acknowledged[messageID]++
			}
		case mtprototl.NewSessionCreatedID:
		default:
			t.Fatalf("unexpected response constructor %08x", binaryConstructor(inner.Data))
		}
	}
	for _, requestID := range requestIDs {
		if results[requestID] != 1 || acknowledged[requestID] != 1 {
			t.Fatalf("request %d correlation counts = result:%d ack:%d", requestID, results[requestID], acknowledged[requestID])
		}
	}
	if len(results) != len(requestIDs) || len(acknowledged) != len(requestIDs) {
		t.Fatalf("unexpected correlated IDs: results=%v acknowledgements=%v", results, acknowledged)
	}
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
