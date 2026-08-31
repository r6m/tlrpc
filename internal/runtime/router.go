package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/session"
)

var (
	ErrControlRouterMissing         = errors.New("runtime: control router is required")
	ErrApplicationDispatcherMissing = errors.New("runtime: application dispatcher is required")
	ErrInvalidSessionMutation       = errors.New("runtime: invalid session mutation")
)

// RequestInfo is immutable framework metadata supplied to protocol and
// application dispatch. It contains no mutable session pointer or raw transport.
type RequestInfo struct {
	ConnectionID uint64
	AuthKeyID    crypto.KeyID
	SessionID    int64
	ServerSalt   int64
	UserID       int64
	Layer        int
	Client       session.ClientMetadata
	Peer         PeerInfo
	Sender       Sender
}

// Sender is the only application-visible route for asynchronous server push.
// It accepts semantic TL bodies and never exposes a transport or wire state.
type Sender interface {
	Push(ctx context.Context, body []byte) error
}

type PeerInfo struct {
	Transport  string
	LocalAddr  string
	RemoteAddr string
}

type Request struct {
	Message InboundMessage
	Info    RequestInfo
}

// ControlRouter owns MTProto controls and wrappers. handled=false delegates the
// message to the generated application dispatcher.
type ControlRouter interface {
	RouteControl(ctx context.Context, request Request) (outcome Outcome, handled bool, err error)
}

// ApplicationDispatcher invokes only user-implemented generated services.
type ApplicationDispatcher interface {
	DispatchApplication(ctx context.Context, request Request) (Outcome, error)
}

// Router enforces protocol-control precedence and validates every outcome
// before it can mutate a lease or enter the writer.
type Router struct {
	controls    ControlRouter
	application ApplicationDispatcher
}

func NewRouter(controls ControlRouter, application ApplicationDispatcher) (*Router, error) {
	if controls == nil {
		return nil, ErrControlRouterMissing
	}
	if application == nil {
		return nil, ErrApplicationDispatcherMissing
	}
	return &Router{controls: controls, application: application}, nil
}

func (r *Router) Route(ctx context.Context, request Request) (Outcome, error) {
	outcome, handled, err := r.RouteControl(ctx, request)
	if err != nil {
		return Outcome{}, err
	}
	if handled {
		return outcome, nil
	}
	return r.DispatchApplication(ctx, request)
}

// RouteControl executes only runtime-owned MTProto controls. Connection loops
// use this split form so controls such as rpc_drop_answer can be consumed while
// an application handler is still running.
func (r *Router) RouteControl(ctx context.Context, request Request) (Outcome, bool, error) {
	if err := request.Message.Validate(); err != nil {
		return Outcome{}, false, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	outcome, handled, err := r.controls.RouteControl(ctx, request)
	if err != nil {
		return Outcome{}, handled, err
	}
	if !handled {
		return Outcome{}, false, nil
	}
	if err := ValidateOutcome(outcome); err != nil {
		return Outcome{}, true, err
	}
	return outcome, true, nil
}

// DispatchApplication invokes only generated user service dispatch after the
// connection loop has established an active-request cancellation lifetime.
func (r *Router) DispatchApplication(ctx context.Context, request Request) (Outcome, error) {
	if err := request.Message.Validate(); err != nil {
		return Outcome{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	outcome, err := r.application.DispatchApplication(ctx, request)
	if err != nil {
		return Outcome{}, err
	}
	if err := ValidateOutcome(outcome); err != nil {
		return Outcome{}, err
	}
	return outcome, nil
}

func ValidateOutcome(outcome Outcome) error {
	for _, intent := range outcome.Intents {
		if err := ValidateIntent(intent); err != nil {
			return err
		}
	}
	for _, mutation := range outcome.Mutations {
		if err := validateSessionMutation(mutation); err != nil {
			return err
		}
	}
	return nil
}

// ApplySessionMutations stages a new detached snapshot. The caller commits it
// through its exclusive lease; this function never persists or mutates shared
// state itself.
func ApplySessionMutations(current session.Snapshot, mutations []SessionMutation) (session.Snapshot, error) {
	next := current.Clone()
	for _, mutation := range mutations {
		if err := validateSessionMutation(mutation); err != nil {
			return session.Snapshot{}, err
		}
		switch value := mutation.(type) {
		case SetLayer:
			next.Layer = value.Layer
		case SetClientMetadata:
			next.Client = session.ClientMetadata{
				APIID: value.APIID, DeviceModel: value.DeviceModel,
				SystemVersion: value.SystemVersion, AppVersion: value.AppVersion,
				SystemLangCode: value.SystemLangCode, LangPack: value.LangPack,
				LangCode: value.LangCode,
			}
		case BindUser:
			next.UserID = value.UserID
		case UnbindUser:
			next.UserID = 0
		case MarkNewSessionCreated:
			next.NewSessionCreated = true
			next.FirstClientMsgID = value.FirstMessageID
		default:
			return session.Snapshot{}, fmt.Errorf("%w: %T", ErrInvalidSessionMutation, mutation)
		}
	}
	return next, nil
}

func validateSessionMutation(mutation SessionMutation) error {
	if mutation == nil {
		return ErrInvalidSessionMutation
	}
	switch value := mutation.(type) {
	case SetLayer:
		if value.Layer <= 0 {
			return ErrInvalidSessionMutation
		}
	case SetClientMetadata:
		if value.APIID <= 0 || value.DeviceModel == "" || value.SystemVersion == "" || value.AppVersion == "" || value.LangCode == "" {
			return ErrInvalidSessionMutation
		}
	case BindUser:
		if value.UserID <= 0 {
			return ErrInvalidSessionMutation
		}
	case UnbindUser:
	case MarkNewSessionCreated:
		if value.FirstMessageID == 0 {
			return ErrInvalidSessionMutation
		}
	default:
		return fmt.Errorf("%w: %T", ErrInvalidSessionMutation, mutation)
	}
	return nil
}
