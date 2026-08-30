package runtime

import (
	"bytes"
	"errors"
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
