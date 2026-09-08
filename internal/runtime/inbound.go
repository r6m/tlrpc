package runtime

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/mtproto/protocol"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
)

var (
	ErrInboundBodyTooShort   = errors.New("runtime: inbound TL body is too short")
	ErrTrailingContainerData = errors.New("runtime: trailing data after MTProto container")
)

type ValidatedInbound struct {
	OuterMessageID int64
	Envelope       InboundMessage
	Messages       []InboundMessage
	Snapshot       session.Snapshot
}

type SessionValidator struct {
	mu        sync.Mutex
	clock     func() time.Time
	limits    mtproto.DecodeLimits
	validator *protocol.Validator
}

func NewSessionValidator(snapshot session.Snapshot, clock func() time.Time) (*SessionValidator, error) {
	return NewSessionValidatorWithLimits(snapshot, clock, mtproto.DecodeLimits{})
}

func NewSessionValidatorWithLimits(snapshot session.Snapshot, clock func() time.Time, limits mtproto.DecodeLimits) (*SessionValidator, error) {
	if _, err := mtproto.NewDecodeBudget(limits); err != nil {
		return nil, err
	}
	validator, err := newProtocolValidator(snapshot, clock)
	if err != nil {
		return nil, err
	}
	return &SessionValidator{clock: clock, limits: limits, validator: validator}, nil
}

func (v *SessionValidator) Validate(snapshot session.Snapshot, inner *mtproto.InnerData) (ValidatedInbound, error) {
	if inner == nil || len(inner.Data) < 4 {
		return ValidatedInbound{}, ErrInboundBodyTooShort
	}
	budget, err := mtproto.NewDecodeBudget(v.limits)
	if err != nil {
		return ValidatedInbound{}, err
	}
	message, decoded, err := classifyProtocolMessage(inner, budget)
	if err != nil {
		return ValidatedInbound{}, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	validator, err := newProtocolValidator(snapshot, v.clock)
	if err != nil {
		return ValidatedInbound{}, err
	}
	v.validator = validator
	// A remembered child can be a legitimate retransmission alongside fresh
	// requests. Validate the entire remaining envelope before exposing either
	// list; a malformed sibling must not partially commit or dispatch.
	recent := make(map[int64]bool, len(snapshot.RecentClientMsgIDs))
	for _, id := range snapshot.RecentClientMsgIDs {
		recent[id] = true
	}
	original := append([]InboundMessage(nil), decoded...)
	replayed := make(map[int64]bool)
	for {
		err := validator.Validate(message)
		if err == nil {
			break
		}
		var bad *protocol.BadMessageError
		if message.Kind != protocol.Container || !errors.As(err, &bad) ||
			!errors.Is(bad.Cause, protocol.ErrReplayMessageID) ||
			bad.MessageID == message.MessageID || !recent[bad.MessageID] {
			return ValidatedInbound{}, err
		}
		index := -1
		for i, child := range message.Children {
			if child.MessageID == bad.MessageID {
				index = i
				break
			}
		}
		if index < 0 {
			return ValidatedInbound{}, err
		}
		replayed[bad.MessageID] = true
		message.Children = append(message.Children[:index], message.Children[index+1:]...)
		decoded = append(decoded[:index], decoded[index+1:]...)
	}
	for i := range original {
		original[i].Retransmission = replayed[original[i].MessageID]
	}
	state := validator.Snapshot()
	next := snapshot.Clone()
	next.SessionID = state.SessionID
	next.ServerSalt = state.ServerSalt
	next.SeqNo = state.SequenceNo
	next.LastClientMsgID = state.HighestMessageID
	next.ClientMsgIDFloor = state.MessageIDFloor
	next.RecentClientMsgIDs = append([]int64(nil), state.RecentMessageIDs...)
	next.RecentClientSeqNos = append([]int32(nil), state.RecentSequenceNos...)
	return ValidatedInbound{
		OuterMessageID: inner.MsgID,
		Envelope: InboundMessage{
			MessageID: inner.MsgID, SequenceNo: inner.SeqNo,
			ConstructorID:  binary.LittleEndian.Uint32(inner.Data[:4]),
			Body:           append([]byte(nil), inner.Data...),
			DecodeBudget:   budget,
			ContentRelated: requiresAcknowledgement(binary.LittleEndian.Uint32(inner.Data[:4]), message.Kind),
		},
		Messages: original, Snapshot: next,
	}, nil
}

func newProtocolValidator(snapshot session.Snapshot, clock func() time.Time) (*protocol.Validator, error) {
	return protocol.NewValidator(protocol.Config{
		SessionID: snapshot.SessionID, ServerSalt: snapshot.ServerSalt,
		SequenceNo: snapshot.SeqNo, HighestMessageID: snapshot.LastClientMsgID,
		MessageIDFloor: snapshot.ClientMsgIDFloor, RecentMessageIDs: snapshot.RecentClientMsgIDs,
		RecentSequenceNos: snapshot.RecentClientSeqNos, Clock: clock,
	})
}

func classifyProtocolMessage(inner *mtproto.InnerData, budget *mtproto.DecodeBudget) (protocol.Message, []InboundMessage, error) {
	constructorID := binary.LittleEndian.Uint32(inner.Data[:4])
	message := protocol.Message{
		ServerSalt: inner.Salt, SessionID: inner.SessionID,
		MessageID: inner.MsgID, SequenceNo: inner.SeqNo,
		Kind: classifyMessageKind(constructorID, inner.SeqNo),
	}
	if constructorID != mtprototl.MsgContainerID {
		return message, []InboundMessage{{
			MessageID: inner.MsgID, SequenceNo: inner.SeqNo,
			ConstructorID: constructorID, Body: append([]byte(nil), inner.Data...),
			ContentRelated: requiresAcknowledgement(constructorID, message.Kind), DecodeBudget: budget,
		}}, nil
	}

	container := &mtprototl.MsgContainer{}
	reader := mtproto.NewBudgetReader(bytes.NewReader(inner.Data), budget)
	if err := mtproto.ConsumeContainer(reader); err != nil {
		return protocol.Message{}, nil, err
	}
	if err := container.DeserializeTL(reader); err != nil {
		return protocol.Message{}, nil, err
	}
	if reader.Len() != 0 {
		return protocol.Message{}, nil, ErrTrailingContainerData
	}
	message.Children = make([]protocol.ContainerMessage, 0, len(container.Messages))
	decoded := make([]InboundMessage, 0, len(container.Messages))
	for _, child := range container.Messages {
		if len(child.BodyRaw) < 4 {
			return protocol.Message{}, nil, ErrInboundBodyTooShort
		}
		childConstructor := binary.LittleEndian.Uint32(child.BodyRaw[:4])
		kind := classifyMessageKind(childConstructor, child.SeqNo)
		message.Children = append(message.Children, protocol.ContainerMessage{
			MessageID: child.MsgID, SequenceNo: child.SeqNo, Kind: kind,
		})
		decoded = append(decoded, InboundMessage{
			MessageID: child.MsgID, SequenceNo: child.SeqNo,
			ConstructorID: childConstructor, Body: append([]byte(nil), child.BodyRaw...),
			ContentRelated: requiresAcknowledgement(childConstructor, kind), DecodeBudget: budget,
		})
	}
	return message, decoded, nil
}

func classifyMessageKind(constructorID uint32, sequenceNo int32) protocol.MessageKind {
	if constructorID == mtprototl.MsgContainerID {
		return protocol.Container
	}
	switch constructorID {
	case mtprototl.PingID, mtprototl.PingDelayDisconnectID:
		// Web K currently allocates these controls through its content-related
		// call path and advances its sequence counter, while Android sends the
		// canonical even form. Match only these constructors to their wire
		// parity so both clients retain coherent sequence progress.
		if sequenceNo&1 != 0 {
			return protocol.ContentRelated
		}
		return protocol.NonContentRelated
	case mtprototl.MsgsAckID,
		mtprototl.NewSessionCreatedID,
		mtprototl.GetFutureSaltsID,
		mtprototl.MsgsStateReqID,
		mtprototl.MsgResendReqID,
		mtprototl.MsgsStateInfoID:
		return protocol.NonContentRelated
	default:
		return protocol.ContentRelated
	}
}

func requiresAcknowledgement(constructorID uint32, kind protocol.MessageKind) bool {
	switch constructorID {
	case mtprototl.PingID, mtprototl.PingDelayDisconnectID:
		return false
	default:
		return kind == protocol.ContentRelated
	}
}
