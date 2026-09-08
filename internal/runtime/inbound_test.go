package runtime

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/mtproto/protocol"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
)

const (
	inboundNowSeconds = int64(10_000)
	inboundSalt       = int64(71)
	inboundSessionID  = int64(83)
)

func TestSessionValidatorAdvancesDetachedSnapshot(t *testing.T) {
	original := inboundSnapshot()
	validator := newInboundValidator(t, original)
	messageID := inboundMessageID(4)

	validated, err := validator.Validate(original, &mtproto.InnerData{
		Salt: inboundSalt, SessionID: inboundSessionID,
		MsgID: messageID, SeqNo: 1, Data: constructorBody(0x01020304),
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if validated.OuterMessageID != messageID || len(validated.Messages) != 1 {
		t.Fatalf("validated inbound = %+v", validated)
	}
	message := validated.Messages[0]
	if message.MessageID != messageID || message.SequenceNo != 1 || message.ConstructorID != 0x01020304 || !message.ContentRelated {
		t.Fatalf("decoded message = %+v", message)
	}
	if validated.Snapshot.SeqNo != 2 || validated.Snapshot.LastClientMsgID != messageID || !reflect.DeepEqual(validated.Snapshot.RecentClientMsgIDs, []int64{messageID}) {
		t.Fatalf("advanced snapshot = %+v", validated.Snapshot)
	}
	if original.SeqNo != 0 || original.LastClientMsgID != 0 || len(original.RecentClientMsgIDs) != 0 {
		t.Fatalf("input snapshot was mutated: %+v", original)
	}
	validated.Messages[0].Body[0] = 0
}

func TestSessionValidatorTreatsGzipPackedRequestAsContentRelated(t *testing.T) {
	original := inboundSnapshot()
	original.SeqNo = 2
	validator := newInboundValidator(t, original)
	messageID := inboundMessageID(4)

	validated, err := validator.Validate(original, &mtproto.InnerData{
		Salt: inboundSalt, SessionID: inboundSessionID,
		MsgID: messageID, SeqNo: 3, Data: constructorBody(mtprototl.GzipPackedID),
	})
	if err != nil {
		t.Fatalf("validate gzip_packed request: %v", err)
	}
	if len(validated.Messages) != 1 || !validated.Messages[0].ContentRelated {
		t.Fatalf("decoded gzip_packed message = %+v", validated.Messages)
	}
	if validated.Snapshot.SeqNo != 4 {
		t.Fatalf("advanced snapshot sequence = %d, want 4", validated.Snapshot.SeqNo)
	}
}

func TestSessionValidatorTreatsPingControlsAsNonContentRelated(t *testing.T) {
	controls := []struct {
		name      string
		messageID int64
		sequence  int32
		wantSeqNo int32
		body      []byte
	}{
		{name: "Android even ping", messageID: inboundMessageID(4), sequence: 6, wantSeqNo: 6, body: serializeInboundControl(t, &mtprototl.Ping{PingID: 1})},
		{name: "Web K odd ping", messageID: inboundMessageID(8), sequence: 7, wantSeqNo: 8, body: serializeInboundControl(t, &mtprototl.Ping{PingID: 2})},
		{name: "Android even ping delay", messageID: inboundMessageID(12), sequence: 6, wantSeqNo: 6, body: serializeInboundControl(t, &mtprototl.PingDelayDisconnect{PingID: 3, DisconnectDelay: 75})},
		{name: "Web K odd ping delay", messageID: inboundMessageID(16), sequence: 9, wantSeqNo: 10, body: serializeInboundControl(t, &mtprototl.PingDelayDisconnect{PingID: 4, DisconnectDelay: 75})},
	}
	for _, control := range controls {
		t.Run(control.name, func(t *testing.T) {
			snapshot := inboundSnapshot()
			snapshot.SeqNo = 6
			validator := newInboundValidator(t, snapshot)
			validated, err := validator.Validate(snapshot, &mtproto.InnerData{
				Salt: inboundSalt, SessionID: inboundSessionID,
				MsgID: control.messageID, SeqNo: control.sequence, Data: control.body,
			})
			if err != nil {
				t.Fatalf("validate %08x: %v", binaryConstructor(control.body), err)
			}
			if len(validated.Messages) != 1 || validated.Messages[0].ContentRelated {
				t.Fatalf("decoded %08x = %+v", binaryConstructor(control.body), validated.Messages)
			}
			if validated.Snapshot.SeqNo != control.wantSeqNo {
				t.Fatalf("sequence state = %d, want %d", validated.Snapshot.SeqNo, control.wantSeqNo)
			}
		})
	}
}

func TestSessionValidatorAcceptsWebKOddPingBesideInitializationRPC(t *testing.T) {
	original := inboundSnapshot()
	validator := newInboundValidator(t, original)
	pingID := inboundMessageID(4)
	configID := inboundMessageID(8)
	outerID := inboundMessageID(12)
	body := serializeInboundContainer(t, []mtprototl.Message{
		{MsgID: pingID, SeqNo: 1, BodyRaw: serializeInboundControl(t, &mtprototl.PingDelayDisconnect{PingID: 9, DisconnectDelay: 75})},
		{MsgID: configID, SeqNo: 3, BodyRaw: constructorBody(mtprototl.InvokeWithLayerID)},
	})

	validated, err := validator.Validate(original, &mtproto.InnerData{
		Salt: inboundSalt, SessionID: inboundSessionID,
		MsgID: outerID, SeqNo: 4, Data: body,
	})
	if err != nil {
		t.Fatalf("validate Web K initialization container: %v", err)
	}
	if len(validated.Messages) != 2 || validated.Messages[0].ContentRelated || !validated.Messages[1].ContentRelated {
		t.Fatalf("classified children = %+v", validated.Messages)
	}
	if validated.Snapshot.SeqNo != 4 {
		t.Fatalf("content sequence = %d, want 4 after odd ping and initialization RPC", validated.Snapshot.SeqNo)
	}
	wantIDs := []int64{outerID, pingID, configID}
	if !reflect.DeepEqual(validated.Snapshot.RecentClientMsgIDs, wantIDs) {
		t.Fatalf("recent IDs = %v, want %v", validated.Snapshot.RecentClientMsgIDs, wantIDs)
	}
}

func TestSessionValidatorRejectsReplayedWebKOddPing(t *testing.T) {
	snapshot := inboundSnapshot()
	validator := newInboundValidator(t, snapshot)
	messageID := inboundMessageID(4)
	inner := &mtproto.InnerData{
		Salt: inboundSalt, SessionID: inboundSessionID,
		MsgID: messageID, SeqNo: 1,
		Data: serializeInboundControl(t, &mtprototl.PingDelayDisconnect{PingID: 9, DisconnectDelay: 75}),
	}
	validated, err := validator.Validate(snapshot, inner)
	if err != nil {
		t.Fatalf("validate first odd ping: %v", err)
	}
	if validated.Messages[0].ContentRelated || validated.Snapshot.SeqNo != 2 {
		t.Fatalf("first odd ping = messages %+v snapshot %+v", validated.Messages, validated.Snapshot)
	}
	_, err = validator.Validate(validated.Snapshot, inner)
	var bad *protocol.BadMessageError
	if !errors.As(err, &bad) || bad.Code != protocol.CodeReplayMessageID || bad.MessageID != messageID || bad.SequenceNo != 1 {
		t.Fatalf("replayed odd ping error = %v", err)
	}
}

func TestSessionValidatorStillRejectsEvenApplicationRPC(t *testing.T) {
	snapshot := inboundSnapshot()
	validator := newInboundValidator(t, snapshot)
	messageID := inboundMessageID(4)
	_, err := validator.Validate(snapshot, &mtproto.InnerData{
		Salt: inboundSalt, SessionID: inboundSessionID,
		MsgID: messageID, SeqNo: 2, Data: constructorBody(0x01020304),
	})
	var bad *protocol.BadMessageError
	if !errors.As(err, &bad) || bad.Code != protocol.CodeExpectedOddSequenceNo || bad.MessageID != messageID || bad.SequenceNo != 2 {
		t.Fatalf("even application RPC error = %v", err)
	}
}

func TestSessionValidatorStillRejectsOddAcknowledgementAtomically(t *testing.T) {
	original := inboundSnapshot()
	validator := newInboundValidator(t, original)
	ackID := inboundMessageID(4)
	body := serializeInboundContainer(t, []mtprototl.Message{
		{MsgID: ackID, SeqNo: 1, BodyRaw: constructorBody(mtprototl.MsgsAckID)},
		{MsgID: inboundMessageID(8), SeqNo: 3, BodyRaw: constructorBody(mtprototl.InvokeWithLayerID)},
	})

	_, err := validator.Validate(original, &mtproto.InnerData{
		Salt: inboundSalt, SessionID: inboundSessionID,
		MsgID: inboundMessageID(12), SeqNo: 4, Data: body,
	})
	var bad *protocol.BadMessageError
	if !errors.As(err, &bad) || bad.Code != protocol.CodeExpectedEvenSequenceNo || bad.MessageID != ackID {
		t.Fatalf("odd acknowledgement error = %v", err)
	}
	state := validator.validator.Snapshot()
	if state.SequenceNo != 0 || state.HighestMessageID != 0 || len(state.RecentMessageIDs) != 0 {
		t.Fatalf("rejected container advanced state: %+v", state)
	}
}

func TestSessionValidatorValidatesContainerAtomically(t *testing.T) {
	original := inboundSnapshot()
	validator := newInboundValidator(t, original)
	outerID := inboundMessageID(20)
	body := serializeInboundContainer(t, []mtprototl.Message{
		{MsgID: inboundMessageID(4), SeqNo: 1, BodyRaw: constructorBody(0x11111111)},
		{MsgID: inboundMessageID(8), SeqNo: 2, BodyRaw: constructorBody(mtprototl.MsgsAckID)},
		{MsgID: inboundMessageID(12), SeqNo: 3, BodyRaw: constructorBody(0x22222222)},
	})

	validated, err := validator.Validate(original, &mtproto.InnerData{
		Salt: inboundSalt, SessionID: inboundSessionID,
		MsgID: outerID, SeqNo: 4, Data: body,
	})
	if err != nil {
		t.Fatalf("validate container: %v", err)
	}
	if len(validated.Messages) != 3 || !validated.Messages[0].ContentRelated || validated.Messages[1].ContentRelated || !validated.Messages[2].ContentRelated {
		t.Fatalf("decoded children = %+v", validated.Messages)
	}
	wantIDs := []int64{outerID, inboundMessageID(4), inboundMessageID(8), inboundMessageID(12)}
	if validated.Snapshot.SeqNo != 4 || !reflect.DeepEqual(validated.Snapshot.RecentClientMsgIDs, wantIDs) {
		t.Fatalf("container snapshot = %+v", validated.Snapshot)
	}
}

func TestSessionValidatorRejectsMalformedContainerWithoutAdvancing(t *testing.T) {
	original := inboundSnapshot()
	validator := newInboundValidator(t, original)

	valid := serializeInboundContainer(t, []mtprototl.Message{
		{MsgID: inboundMessageID(4), SeqNo: 1, BodyRaw: constructorBody(0x11111111)},
		{MsgID: inboundMessageID(8), SeqNo: 2, BodyRaw: constructorBody(mtprototl.MsgContainerID)},
	})
	_, err := validator.Validate(original, &mtproto.InnerData{
		Salt: inboundSalt, SessionID: inboundSessionID,
		MsgID: inboundMessageID(20), SeqNo: 4, Data: valid,
	})
	if !errors.Is(err, protocol.ErrInvalidMessageKind) {
		t.Fatalf("nested container error = %v", err)
	}

	valid = append(serializeInboundContainer(t, []mtprototl.Message{
		{MsgID: inboundMessageID(4), SeqNo: 1, BodyRaw: constructorBody(0x11111111)},
	}), 0xff)
	_, err = validator.Validate(original, &mtproto.InnerData{
		Salt: inboundSalt, SessionID: inboundSessionID,
		MsgID: inboundMessageID(24), SeqNo: 2, Data: valid,
	})
	if !errors.Is(err, ErrTrailingContainerData) {
		t.Fatalf("trailing container error = %v", err)
	}

	state := validator.validator.Snapshot()
	if state.SequenceNo != 0 || state.HighestMessageID != 0 || len(state.RecentMessageIDs) != 0 {
		t.Fatalf("invalid container advanced validator: %+v", state)
	}
}

func TestSessionValidatorReturnsCanonicalBadMessageErrors(t *testing.T) {
	original := inboundSnapshot()
	validator := newInboundValidator(t, original)
	inner := &mtproto.InnerData{
		Salt: inboundSalt + 1, SessionID: inboundSessionID,
		MsgID: inboundMessageID(4), SeqNo: 1, Data: constructorBody(0x01020304),
	}
	_, err := validator.Validate(original, inner)
	var bad *protocol.BadMessageError
	if !errors.As(err, &bad) || bad.Code != protocol.CodeBadServerSalt || bad.ExpectedServerSalt != inboundSalt {
		t.Fatalf("bad salt error = %#v", err)
	}

	inner.Salt = inboundSalt
	inner.SessionID++
	inner.MsgID = inboundMessageID(8)
	_, err = validator.Validate(original, inner)
	if !errors.As(err, &bad) || bad.Code != protocol.CodeSessionIDMismatch {
		t.Fatalf("bad session error = %#v", err)
	}
}

func TestSessionValidatorPersistsFullWindowReplayStateAcrossRestart(t *testing.T) {
	snapshot := inboundSnapshot()
	validator := newInboundValidator(t, snapshot)
	firstID := inboundMessageID(4)
	for index := 0; index < protocol.DefaultRecentMessageIDLimit+1; index++ {
		messageID := inboundMessageID(uint32((index + 1) * 4))
		validated, err := validator.Validate(snapshot, &mtproto.InnerData{
			Salt: inboundSalt, SessionID: inboundSessionID,
			MsgID: messageID, SeqNo: int32(index*2 + 1), Data: constructorBody(0x01020304),
		})
		if err != nil {
			t.Fatalf("validate message %d: %v", index, err)
		}
		snapshot = validated.Snapshot
	}
	if snapshot.ClientMsgIDFloor != firstID || len(snapshot.RecentClientSeqNos) == 0 {
		t.Fatalf("durable replay state = %+v", snapshot)
	}

	restored := newInboundValidator(t, snapshot)
	_, err := restored.Validate(snapshot, &mtproto.InnerData{
		Salt: inboundSalt, SessionID: inboundSessionID,
		MsgID: firstID, SeqNo: 1, Data: constructorBody(0x01020304),
	})
	var bad *protocol.BadMessageError
	if !errors.As(err, &bad) || bad.Code != protocol.CodeReplayMessageID {
		t.Fatalf("replay after restart error = %v", err)
	}
}

func inboundSnapshot() session.Snapshot {
	return session.Snapshot{
		SessionID: inboundSessionID, ServerSalt: inboundSalt,
	}
}

func newInboundValidator(t *testing.T, snapshot session.Snapshot) *SessionValidator {
	t.Helper()
	validator, err := NewSessionValidator(snapshot, func() time.Time { return time.Unix(inboundNowSeconds, 0) })
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}
	return validator
}

func inboundMessageID(low uint32) int64 {
	return inboundNowSeconds<<32 | int64(low)
}

func serializeInboundContainer(t *testing.T, messages []mtprototl.Message) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := (&mtprototl.MsgContainer{Messages: messages}).SerializeTL(&buffer); err != nil {
		t.Fatalf("serialize container: %v", err)
	}
	return buffer.Bytes()
}

func serializeInboundControl(t *testing.T, value interface{ SerializeTL(io.Writer) error }) []byte {
	t.Helper()
	body, err := serializeRuntimeTL(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
