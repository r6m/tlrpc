package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
)

var (
	ErrControlDependencies = errors.New("runtime: incomplete control router dependencies")
	ErrTrailingControlData = errors.New("runtime: trailing data after MTProto control object")
)

type OutboundReliability interface {
	AcknowledgeOutbound(ctx context.Context, messageIDs []int64) error
	InspectOutbound(ctx context.Context, messageID int64) (OutboundReliabilityState, error)
}

type InboundStateSource interface {
	StateInfo(messageIDs []int64) []byte
}

type MTProtoControlRouter struct {
	outbound OutboundReliability
	inbound  InboundStateSource
	active   *ActiveRequestRegistry
	now      func() time.Time
}

type MTProtoControlConfig struct {
	Outbound OutboundReliability
	Inbound  InboundStateSource
	Active   *ActiveRequestRegistry
	Now      func() time.Time
}

func NewMTProtoControlRouter(config MTProtoControlConfig) (*MTProtoControlRouter, error) {
	if config.Outbound == nil || config.Inbound == nil || config.Active == nil {
		return nil, ErrControlDependencies
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &MTProtoControlRouter{outbound: config.Outbound, inbound: config.Inbound, active: config.Active, now: config.Now}, nil
}

func (r *MTProtoControlRouter) RouteControl(ctx context.Context, request Request) (Outcome, bool, error) {
	switch request.Message.ConstructorID {
	case mtprototl.MsgsAckID:
		message := &mtprototl.MsgsAck{}
		if err := decodeControlBudget(request.Message.Body, message, request.Message.DecodeBudget); err != nil {
			return Outcome{}, true, err
		}
		if err := r.outbound.AcknowledgeOutbound(ctx, message.MsgIDs); err != nil {
			return Outcome{}, true, err
		}
		return Outcome{}, true, nil

	case mtprototl.MsgsStateReqID:
		message := &mtprototl.MsgsStateReq{}
		if err := decodeControlBudget(request.Message.Body, message, request.Message.DecodeBudget); err != nil {
			return Outcome{}, true, err
		}
		body, err := serializeRuntimeTL(&mtprototl.MsgsStateInfo{
			ReqMsgID: request.Message.MessageID,
			Info:     r.inbound.StateInfo(message.MsgIDs),
		})
		if err != nil {
			return Outcome{}, true, err
		}
		return Outcome{Intents: []Intent{ProtocolReply{Body: body}}}, true, nil

	case mtprototl.MsgResendReqID:
		message := &mtprototl.MsgResendReq{}
		if err := decodeControlBudget(request.Message.Body, message, request.Message.DecodeBudget); err != nil {
			return Outcome{}, true, err
		}
		if len(message.MsgIDs) == 0 {
			return Outcome{}, true, nil
		}
		states := make([]OutboundReliabilityState, len(message.MsgIDs))
		allEligible := true
		for index, messageID := range message.MsgIDs {
			state, err := r.outbound.InspectOutbound(ctx, messageID)
			if err != nil {
				return Outcome{}, true, err
			}
			states[index] = state
			allEligible = allEligible && state.ResendEligible
		}
		if allEligible {
			return Outcome{Intents: []Intent{Resend{MessageIDs: append([]int64(nil), message.MsgIDs...)}}}, true, nil
		}
		info := make([]byte, len(states))
		for index, state := range states {
			if !state.Known {
				info[index] = mtprototl.MessageStateUnknownTooOld
				continue
			}
			info[index] = mtprototl.MessageStateReceived | mtprototl.MessageStateKnown
			if state.Acknowledged {
				info[index] |= mtprototl.MessageStateAcknowledged
			}
		}
		body, err := serializeRuntimeTL(&mtprototl.MsgsStateInfo{
			ReqMsgID: request.Message.MessageID,
			Info:     info,
		})
		if err != nil {
			return Outcome{}, true, err
		}
		return Outcome{Intents: []Intent{ProtocolReply{Body: body}}}, true, nil

	case mtprototl.RPCDropAnswerID:
		message := &mtprototl.RPCDropAnswer{}
		if err := decodeControlBudget(request.Message.Body, message, request.Message.DecodeBudget); err != nil {
			return Outcome{}, true, err
		}
		var answer interface{ SerializeTL(io.Writer) error } = &mtprototl.RPCAnswerUnknown{}
		if r.active.Drop(message.ReqMsgID) == DropStatusDroppedRunning {
			answer = &mtprototl.RPCAnswerDroppedRunning{}
		}
		body, err := serializeRuntimeTL(answer)
		if err != nil {
			return Outcome{}, true, err
		}
		return Outcome{Intents: []Intent{RPCResult{
			RequestMessageID: request.Message.MessageID,
			Body:             body,
		}}}, true, nil

	case mtprototl.GetFutureSaltsID:
		message := &mtprototl.GetFutureSaltsRequest{}
		if err := decodeControlBudget(request.Message.Body, message, request.Message.DecodeBudget); err != nil {
			return Outcome{}, true, err
		}
		count := int(message.Num)
		if count <= 0 {
			count = 1
		}
		if count > mtprototl.MaxFutureSalts {
			count = mtprototl.MaxFutureSalts
		}
		now := int32(r.now().Unix())
		salts := make([]mtprototl.FutureSalt, count)
		for index := range salts {
			salts[index] = mtprototl.FutureSalt{
				ValidSince: now + int32(index*1800),
				ValidUntil: now + int32((index+1)*1800),
				Salt:       request.Info.ServerSalt,
			}
		}
		body, err := serializeRuntimeTL(&mtprototl.FutureSalts{
			ReqMsgID: request.Message.MessageID, Now: now, Salts: salts,
		})
		if err != nil {
			return Outcome{}, true, err
		}
		return Outcome{Intents: []Intent{ProtocolReply{Body: body}}}, true, nil
	default:
		return Outcome{}, false, nil
	}
}

func decodeControl(body []byte, value interface{ DeserializeTL(io.Reader) error }) error {
	return decodeControlBudget(body, value, nil)
}

func decodeControlBudget(body []byte, value interface{ DeserializeTL(io.Reader) error }, budget *mtproto.DecodeBudget) error {
	base := bytes.NewReader(body)
	var reader interface {
		io.Reader
		Len() int
	} = base
	if budget != nil {
		reader = mtproto.NewBudgetReader(base, budget)
	}
	if err := value.DeserializeTL(reader); err != nil {
		return err
	}
	if reader.Len() != 0 {
		return fmt.Errorf("%w: %d bytes", ErrTrailingControlData, reader.Len())
	}
	return nil
}
