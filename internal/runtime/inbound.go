package runtime

import (
	"bytes"
	"encoding/binary"
	"errors"
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
	validator *protocol.Validator
}

func NewSessionValidator(snapshot session.Snapshot, clock func() time.Time) (*SessionValidator, error) {
	validator, err := protocol.NewValidator(protocol.Config{
		SessionID: snapshot.SessionID, ServerSalt: snapshot.ServerSalt,
		SequenceNo: snapshot.SeqNo, HighestMessageID: snapshot.LastClientMsgID,
		RecentMessageIDs: snapshot.RecentClientMsgIDs, Clock: clock,
	})
	if err != nil {
		return nil, err
	}
	return &SessionValidator{validator: validator}, nil
}

func (v *SessionValidator) Validate(snapshot session.Snapshot, inner *mtproto.InnerData) (ValidatedInbound, error) {
	if inner == nil || len(inner.Data) < 4 {
		return ValidatedInbound{}, ErrInboundBodyTooShort
	}
	message, decoded, err := classifyProtocolMessage(inner)
	if err != nil {
		return ValidatedInbound{}, err
	}
	if err := v.validator.Validate(message); err != nil {
		return ValidatedInbound{}, err
	}
	state := v.validator.Snapshot()
	next := snapshot.Clone()
	next.SessionID = state.SessionID
	next.ServerSalt = state.ServerSalt
	next.SeqNo = state.SequenceNo
	next.LastClientMsgID = state.HighestMessageID
	next.RecentClientMsgIDs = append([]int64(nil), state.RecentMessageIDs...)
	return ValidatedInbound{
		OuterMessageID: inner.MsgID,
		Envelope: InboundMessage{
			MessageID: inner.MsgID, SequenceNo: inner.SeqNo,
			ConstructorID:  binary.LittleEndian.Uint32(inner.Data[:4]),
			Body:           append([]byte(nil), inner.Data...),
			ContentRelated: message.Kind == protocol.ContentRelated,
		},
		Messages: decoded, Snapshot: next,
	}, nil
}

func classifyProtocolMessage(inner *mtproto.InnerData) (protocol.Message, []InboundMessage, error) {
	constructorID := binary.LittleEndian.Uint32(inner.Data[:4])
	message := protocol.Message{
		ServerSalt: inner.Salt, SessionID: inner.SessionID,
		MessageID: inner.MsgID, SequenceNo: inner.SeqNo,
		Kind: classifyMessageKind(constructorID),
	}
	if constructorID != mtprototl.MsgContainerID {
		return message, []InboundMessage{{
			MessageID: inner.MsgID, SequenceNo: inner.SeqNo,
			ConstructorID: constructorID, Body: append([]byte(nil), inner.Data...),
			ContentRelated: message.Kind == protocol.ContentRelated,
		}}, nil
	}

	container := &mtprototl.MsgContainer{}
	reader := bytes.NewReader(inner.Data)
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
		kind := classifyMessageKind(childConstructor)
		message.Children = append(message.Children, protocol.ContainerMessage{
			MessageID: child.MsgID, SequenceNo: child.SeqNo, Kind: kind,
		})
		decoded = append(decoded, InboundMessage{
			MessageID: child.MsgID, SequenceNo: child.SeqNo,
			ConstructorID: childConstructor, Body: append([]byte(nil), child.BodyRaw...),
			ContentRelated: kind == protocol.ContentRelated,
		})
	}
	return message, decoded, nil
}

func classifyMessageKind(constructorID uint32) protocol.MessageKind {
	if constructorID == mtprototl.MsgContainerID {
		return protocol.Container
	}
	switch constructorID {
	case mtprototl.MsgsAckID,
		mtprototl.GzipPackedID,
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
