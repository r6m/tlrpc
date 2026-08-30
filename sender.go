package tlrpc

import (
	"context"
	"errors"

	runtimev2 "github.com/r6m/tlrpc/internal/runtime"
)

var ErrSenderUnavailable = errors.New("tlrpc: sender is unavailable")

// Sender emits schema-defined server push through Runtime v2's single writer.
// It exposes neither the transport nor MTProto wire state.
type Sender interface {
	Send(ctx context.Context, object TLObject) error
}

// SenderFromContext returns the request-scoped semantic push sender.
func SenderFromContext(ctx context.Context) (Sender, bool) {
	if ctx == nil {
		return nil, false
	}
	sender, ok := ctx.Value(contextKeySender).(Sender)
	return sender, ok
}

func withRuntimeSender(ctx context.Context, sender runtimev2.Sender) context.Context {
	return context.WithValue(ctx, contextKeySender, runtimeSender{sender: sender})
}

type runtimeSender struct{ sender runtimev2.Sender }

func (s runtimeSender) Send(ctx context.Context, object TLObject) error {
	if s.sender == nil {
		return ErrSenderUnavailable
	}
	body, err := encodeTLObject(object)
	if err != nil {
		return err
	}
	return s.sender.Push(ctx, body)
}

var _ Sender = runtimeSender{}
