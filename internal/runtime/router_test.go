package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/session"
)

func TestRouterGivesProtocolControlsPrecedence(t *testing.T) {
	controls := &controlRouterStub{handled: true, outcome: Outcome{Intents: []Intent{Acknowledge{MessageIDs: []int64{1}}}}}
	application := &applicationDispatcherStub{outcome: Outcome{Intents: []Intent{Push{Body: constructorBody(2)}}}}
	router, err := NewRouter(controls, application)
	if err != nil {
		t.Fatalf("NewRouter(): %v", err)
	}
	outcome, err := router.Route(context.Background(), validRuntimeRequest())
	if err != nil {
		t.Fatalf("Route(): %v", err)
	}
	if controls.calls != 1 || application.calls != 0 || len(outcome.Intents) != 1 {
		t.Fatalf("routing calls controls=%d application=%d outcome=%+v", controls.calls, application.calls, outcome)
	}
}

func TestRouterDelegatesUnknownConstructorToApplication(t *testing.T) {
	controls := &controlRouterStub{}
	application := &applicationDispatcherStub{outcome: Outcome{Intents: []Intent{RPCResult{RequestMessageID: 1, Body: constructorBody(2)}}}}
	router, err := NewRouter(controls, application)
	if err != nil {
		t.Fatalf("NewRouter(): %v", err)
	}
	if _, err := router.Route(context.Background(), validRuntimeRequest()); err != nil {
		t.Fatalf("Route(): %v", err)
	}
	if controls.calls != 1 || application.calls != 1 {
		t.Fatalf("routing calls controls=%d application=%d", controls.calls, application.calls)
	}
}

func TestRouterRejectsInvalidOutcomeBeforeWriterOrSession(t *testing.T) {
	router, err := NewRouter(&controlRouterStub{}, &applicationDispatcherStub{outcome: Outcome{
		Intents:   []Intent{Push{}},
		Mutations: []SessionMutation{BindUser{UserID: -1}},
	}})
	if err != nil {
		t.Fatalf("NewRouter(): %v", err)
	}
	if _, err := router.Route(context.Background(), validRuntimeRequest()); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("Route() error = %v, want ErrInvalidIntent", err)
	}
}

func TestRouterSplitRoutingKeepsControlsOutOfApplicationDispatch(t *testing.T) {
	controls := &controlRouterStub{handled: true, outcome: Outcome{Intents: []Intent{Acknowledge{MessageIDs: []int64{1}}}}}
	application := &applicationDispatcherStub{}
	router, err := NewRouter(controls, application)
	if err != nil {
		t.Fatal(err)
	}
	request := validRuntimeRequest()
	outcome, handled, err := router.RouteControl(context.Background(), request)
	if err != nil || !handled || len(outcome.Intents) != 1 {
		t.Fatalf("RouteControl = %+v, %t, %v", outcome, handled, err)
	}
	if application.calls != 0 {
		t.Fatalf("application calls after control = %d", application.calls)
	}

	controls.handled = false
	_, handled, err = router.RouteControl(context.Background(), request)
	if err != nil || handled {
		t.Fatalf("unhandled RouteControl = %t, %v", handled, err)
	}
	if _, err := router.DispatchApplication(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if application.calls != 1 {
		t.Fatalf("application calls = %d, want 1", application.calls)
	}
}

func TestApplySessionMutationsStagesDetachedNamedState(t *testing.T) {
	current := session.Snapshot{AuthKeyID: crypto.KeyID(7), SessionID: 9, RecentClientMsgIDs: []int64{1}}
	next, err := ApplySessionMutations(current, []SessionMutation{
		SetLayer{Layer: 228},
		SetClientMetadata{APIID: 10, DeviceModel: "device", SystemVersion: "system", AppVersion: "app", LangCode: "en"},
		BindUser{UserID: 42},
		MarkNewSessionCreated{FirstMessageID: 100},
	})
	if err != nil {
		t.Fatalf("ApplySessionMutations(): %v", err)
	}
	if next.Layer != 228 || next.UserID != 42 || next.Client.APIID != 10 || !next.NewSessionCreated || next.FirstClientMsgID != 100 {
		t.Fatalf("staged snapshot = %+v", next)
	}
	next.RecentClientMsgIDs[0] = 99
	if current.RecentClientMsgIDs[0] != 1 {
		t.Fatal("mutation result aliases current snapshot")
	}
}

type controlRouterStub struct {
	outcome Outcome
	handled bool
	err     error
	calls   int
}

func (s *controlRouterStub) RouteControl(context.Context, Request) (Outcome, bool, error) {
	s.calls++
	return s.outcome, s.handled, s.err
}

type applicationDispatcherStub struct {
	outcome Outcome
	err     error
	calls   int
}

func (s *applicationDispatcherStub) DispatchApplication(context.Context, Request) (Outcome, error) {
	s.calls++
	return s.outcome, s.err
}

func validRuntimeRequest() Request {
	body := constructorBody(0x01020304)
	return Request{
		Message: InboundMessage{MessageID: 1, SequenceNo: 1, ConstructorID: 0x01020304, Body: body, ContentRelated: true},
		Info:    RequestInfo{AuthKeyID: crypto.KeyID(7), SessionID: 9, Layer: 228},
	}
}
