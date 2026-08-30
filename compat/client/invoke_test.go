package client

import (
	"io"
	"testing"

	"github.com/r6m/tlrpc/mtproto"
)

type queuedPush struct{ id uint32 }

func (p *queuedPush) ConstructorID() uint32 { return p.id }
func (p *queuedPush) SerializeTL(w io.Writer) error {
	return mtproto.WriteUint32(w, p.id)
}

func TestHandleDecodedQueuesPushWhileWaitingForRPCResult(t *testing.T) {
	client := New(nil)
	push := &queuedPush{id: 0x7f001234}
	response, handled, err := client.handleDecoded(push, 99)
	if err != nil || handled || response != nil {
		t.Fatalf("handle push = %#v, %t, %v; want queued", response, handled, err)
	}
	if queued := client.popPush(); queued != push {
		t.Fatalf("queued push = %#v, want original object", queued)
	}
}
