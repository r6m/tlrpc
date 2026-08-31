// Package runtime defines the implementation-neutral contracts between the
// Runtime v2 connection stages. It deliberately does not import the root tlrpc
// package, generated schemas, transports, or cryptography.
package runtime

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/r6m/tlrpc/mtproto"
)

var (
	ErrInvalidInbound = errors.New("runtime: invalid inbound message")
	ErrInvalidIntent  = errors.New("runtime: invalid outbound intent")
)

// InboundMessage is one decoded and protocol-validated message ready for
// control routing or generated application dispatch. Body contains the exact
// bare TL object, including its constructor ID.
type InboundMessage struct {
	MessageID      int64
	SequenceNo     int32
	ConstructorID  uint32
	Body           []byte
	Dependencies   []int64
	DecodeBudget   *mtproto.DecodeBudget
	ContentRelated bool
	SuppressPush   bool
}

func (m InboundMessage) Validate() error {
	if m.MessageID == 0 || m.ConstructorID == 0 || len(m.Body) < 4 || binary.LittleEndian.Uint32(m.Body[:4]) != m.ConstructorID {
		return ErrInvalidInbound
	}
	for _, dependency := range m.Dependencies {
		if dependency == 0 || dependency >= m.MessageID {
			return ErrInvalidInbound
		}
	}
	return nil
}

// Outcome is the complete result of routing one inbound message. Protocol
// state changes are explicit and are committed by the session owner; wire
// effects are submitted to the single writer as intents.
type Outcome struct {
	Intents   []Intent
	Mutations []SessionMutation
}

// Intent is a closed set of semantic outbound operations. Intents never carry
// assigned server message IDs, sequence numbers, encrypted packets, or raw
// transport frames; those belong exclusively to the writer.
type Intent interface {
	isIntent()
}

// RPCResult produces one rpc_result correlated to RequestMessageID.
type RPCResult struct {
	RequestMessageID int64
	Body             []byte
}

func (RPCResult) isIntent() {}

// RPCError produces one rpc_result containing rpc_error.
type RPCError struct {
	RequestMessageID int64
	Code             int32
	Message          string
}

func (RPCError) isIntent() {}

// ProtocolReply emits a runtime-owned MTProto control object.
type ProtocolReply struct {
	Body           []byte
	ContentRelated bool
	Unsolicited    bool
}

func (ProtocolReply) isIntent() {}

// Acknowledge emits msgs_ack for inbound content-related message IDs.
type Acknowledge struct {
	MessageIDs []int64
}

func (Acknowledge) isIntent() {}

// Push emits an uncorrelated application object to the active session.
type Push struct {
	Body []byte
}

func (Push) isIntent() {}

// Batch asks the writer to emit Items as one intentional MTProto container.
// The writer still assigns every child and outer message ID/sequence number.
type Batch struct {
	Items []Intent
}

func (Batch) isIntent() {}

// Resend asks the writer to retransmit retained outbound messages. It does not
// expose retained encrypted packets to control routing.
type Resend struct {
	MessageIDs []int64
}

func (Resend) isIntent() {}

// Close terminates the connection after all earlier accepted intents have
// reached their defined terminal result.
type Close struct {
	Cause error
}

func (Close) isIntent() {}

// SessionMutation is the closed set of durable protocol-session changes that
// routing may request. Arbitrary application data is intentionally absent.
type SessionMutation interface {
	isSessionMutation()
}

type SetLayer struct{ Layer int }

func (SetLayer) isSessionMutation() {}

type SetClientMetadata struct {
	APIID          int32
	DeviceModel    string
	SystemVersion  string
	AppVersion     string
	SystemLangCode string
	LangPack       string
	LangCode       string
}

func (SetClientMetadata) isSessionMutation() {}

type BindUser struct{ UserID int64 }

func (BindUser) isSessionMutation() {}

type UnbindUser struct{}

func (UnbindUser) isSessionMutation() {}

type MarkNewSessionCreated struct {
	FirstMessageID int64
}

func (MarkNewSessionCreated) isSessionMutation() {}

// ValidateIntent rejects semantically incomplete operations before they enter
// the writer queue. It intentionally rejects nested batches so container
// ownership remains one level and deterministic.
func ValidateIntent(intent Intent) error {
	if intent == nil {
		return ErrInvalidIntent
	}
	switch value := intent.(type) {
	case RPCResult:
		if value.RequestMessageID == 0 || len(value.Body) < 4 {
			return ErrInvalidIntent
		}
	case RPCError:
		if value.RequestMessageID == 0 || value.Message == "" {
			return ErrInvalidIntent
		}
	case ProtocolReply:
		if len(value.Body) < 4 {
			return ErrInvalidIntent
		}
	case Acknowledge:
		if err := validateMessageIDs(value.MessageIDs); err != nil {
			return err
		}
	case Push:
		if len(value.Body) < 4 {
			return ErrInvalidIntent
		}
	case Batch:
		if len(value.Items) == 0 {
			return ErrInvalidIntent
		}
		for _, child := range value.Items {
			switch child.(type) {
			case RPCResult, RPCError, ProtocolReply, Acknowledge, Push:
			default:
				return fmt.Errorf("%w: non-message batch child %T", ErrInvalidIntent, child)
			}
			if err := ValidateIntent(child); err != nil {
				return err
			}
		}
	case Resend:
		if err := validateMessageIDs(value.MessageIDs); err != nil {
			return err
		}
	case Close:
		if value.Cause == nil {
			return ErrInvalidIntent
		}
	default:
		return fmt.Errorf("%w: %T", ErrInvalidIntent, intent)
	}
	return nil
}

func validateMessageIDs(ids []int64) error {
	if len(ids) == 0 {
		return ErrInvalidIntent
	}
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			return ErrInvalidIntent
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("%w: duplicate message ID %d", ErrInvalidIntent, id)
		}
		seen[id] = struct{}{}
	}
	return nil
}
