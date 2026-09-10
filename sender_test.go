package tlrpc

import (
	"context"
	"errors"
	"io"
	"testing"

	runtimev2 "github.com/r6m/tlrpc/internal/runtime"
	"github.com/r6m/tlrpc/mtproto"
)

type senderTestRuntimeSender struct {
	pushes int
	body   []byte
}

func (s *senderTestRuntimeSender) Push(_ context.Context, body []byte) error {
	s.pushes++
	s.body = append([]byte(nil), body...)
	return nil
}

var _ runtimev2.Sender = (*senderTestRuntimeSender)(nil)

type senderTestLayerObject struct{ layer int }

func (*senderTestLayerObject) ConstructorID() uint32 { return 0x01020304 }

func (o *senderTestLayerObject) SerializeTL(w io.Writer) error {
	o.layer = mtproto.TLLayer(w)
	return mtproto.WriteUint32(w, o.ConstructorID())
}

func TestRuntimeSenderUsesEffectiveLayer(t *testing.T) {
	for _, layer := range []int{228, 229} {
		t.Run("layer", func(t *testing.T) {
			runtime := &senderTestRuntimeSender{}
			ctx := withRuntimeSender(context.Background(), runtime, layer, EncodeLimits{})
			sender, ok := SenderFromContext(ctx)
			if !ok {
				t.Fatal("SenderFromContext returned no sender")
			}
			object := &senderTestLayerObject{}
			if err := sender.Send(context.Background(), object); err != nil {
				t.Fatalf("send: %v", err)
			}
			if object.layer != layer {
				t.Fatalf("encoder layer = %d, want %d", object.layer, layer)
			}
			if runtime.pushes != 1 {
				t.Fatalf("pushes = %d, want 1", runtime.pushes)
			}
		})
	}
}

type senderTestLargeObject struct{}

func (*senderTestLargeObject) ConstructorID() uint32 { return 0x05060708 }

func (*senderTestLargeObject) SerializeTL(w io.Writer) error {
	_, err := w.Write(make([]byte, 5))
	return err
}

func TestRuntimeSenderEnforcesEncodedByteLimit(t *testing.T) {
	runtime := &senderTestRuntimeSender{}
	sender := runtimeSender{
		sender: runtime,
		limits: EncodeLimits{MaxEncodedBytes: 4},
	}
	if err := sender.Send(context.Background(), &senderTestLargeObject{}); !errors.Is(err, ErrEncodedTLTooLarge) {
		t.Fatalf("send error = %v, want ErrEncodedTLTooLarge", err)
	}
	if runtime.pushes != 0 {
		t.Fatalf("pushes = %d, want 0", runtime.pushes)
	}
}

func TestRuntimeSenderUnavailable(t *testing.T) {
	err := (runtimeSender{}).Send(context.Background(), &senderTestLayerObject{})
	if !errors.Is(err, ErrSenderUnavailable) {
		t.Fatalf("send error = %v, want ErrSenderUnavailable", err)
	}
}
