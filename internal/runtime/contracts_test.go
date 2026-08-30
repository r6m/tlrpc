package runtime

import (
	"errors"
	"testing"
)

func TestInboundMessageValidation(t *testing.T) {
	valid := InboundMessage{MessageID: 1, ConstructorID: 0x01020304, Body: []byte{4, 3, 2, 1}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid inbound message: %v", err)
	}
	for _, invalid := range []InboundMessage{
		{ConstructorID: valid.ConstructorID, Body: valid.Body},
		{MessageID: 1, Body: valid.Body},
		{MessageID: 1, ConstructorID: valid.ConstructorID, Body: []byte{1, 2, 3}},
	} {
		if err := invalid.Validate(); !errors.Is(err, ErrInvalidInbound) {
			t.Fatalf("invalid inbound error = %v, want ErrInvalidInbound", err)
		}
	}
}

func TestValidateIntentAcceptsSemanticOperationsWithoutWireState(t *testing.T) {
	body := []byte{4, 3, 2, 1}
	intents := []Intent{
		RPCResult{RequestMessageID: 1, Body: body},
		RPCError{RequestMessageID: 1, Code: 400, Message: "BAD_REQUEST"},
		ProtocolReply{Body: body},
		Acknowledge{MessageIDs: []int64{1, 2}},
		Push{Body: body},
		Batch{Items: []Intent{RPCResult{RequestMessageID: 1, Body: body}, Push{Body: body}}},
		Resend{MessageIDs: []int64{3}},
		Close{Cause: errors.New("closed")},
	}
	for _, intent := range intents {
		if err := ValidateIntent(intent); err != nil {
			t.Fatalf("ValidateIntent(%T): %v", intent, err)
		}
	}
}

func TestValidateIntentRejectsWireEscapeShapes(t *testing.T) {
	body := []byte{4, 3, 2, 1}
	invalid := []Intent{
		RPCResult{Body: body},
		RPCError{RequestMessageID: 1},
		ProtocolReply{},
		Acknowledge{MessageIDs: []int64{1, 1}},
		Push{},
		Batch{},
		Batch{Items: []Intent{Batch{Items: []Intent{Push{Body: body}}}}},
		Resend{},
		Close{},
	}
	for _, intent := range invalid {
		if err := ValidateIntent(intent); !errors.Is(err, ErrInvalidIntent) {
			t.Fatalf("ValidateIntent(%T) error = %v, want ErrInvalidIntent", intent, err)
		}
	}
}
