package runtime

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
)

type replayTestApplication struct {
	calls   chan int64
	blockID int64
	release chan struct{}
}

func (a *replayTestApplication) DispatchApplication(ctx context.Context, r Request) (Outcome, error) {
	a.calls <- r.Message.MessageID
	if r.Message.MessageID == a.blockID {
		select {
		case <-a.release:
		case <-ctx.Done():
			return Outcome{}, ctx.Err()
		}
	}
	return Outcome{Intents: []Intent{RPCResult{RequestMessageID: r.Message.MessageID, Body: constructorBody(0x20202020)}}}, nil
}
func TestConnectionMixedContainerRetransmission(t *testing.T) {
	for _, mode := range []string{"completed", "repeated", "acknowledged", "ack_in_container", "inflight", "restart"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Unix(inboundNowSeconds, 0)
			oldID, freshID := inboundMessageID(4), inboundMessageID(8)
			app := &replayTestApplication{calls: make(chan int64, 10), release: make(chan struct{})}
			if mode == "inflight" || mode == "restart" {
				app.blockID = oldID
			}
			store := session.NewMemoryStore()
			h := newConnectionHarnessWithStore(t, now, app, 100, nil, store)
			send := func(id int64, seq int32, body []byte) {
				t.Helper()
				if err := h.connection.handleEncrypted(context.Background(), DecodedFrame{Encrypted: &mtproto.InnerData{Salt: inboundSalt, SessionID: inboundSessionID, MsgID: id, SeqNo: seq, Data: body}, AuthKeyID: h.authKey.ID(), AuthKey: h.authKey}); err != nil {
					t.Fatal(err)
				}
			}
			waitCall := func(want int64) {
				t.Helper()
				select {
				case got := <-app.calls:
					if got != want {
						t.Fatalf("handler id=%d want %d (duplicate executed)", got, want)
					}
				case <-time.After(time.Second):
					t.Fatalf("handler %d never called", want)
				}
			}
			send(oldID, 1, constructorBody(0x10101010))
			waitCall(oldID)
			var original []byte
			var responseID int64
			if mode == "completed" || mode == "repeated" || mode == "acknowledged" || mode == "ack_in_container" {
				waitForWrittenFrames(t, h.transport, 3)
				for _, frame := range h.transport.writtenFrames() {
					inner := decryptWriterFrame(t, h.authKey, frame)
					if binaryConstructor(inner.Data) == mtprototl.RPCResultID {
						original = frame
						responseID = inner.MsgID
						if mode == "acknowledged" {
							send(inboundMessageID(6*4), 2, encodeControlBody(t, &mtprototl.MsgsAck{MsgIDs: []int64{inner.MsgID}}))
						}
					}
				}
				if original == nil {
					t.Fatal("missing original response")
				}
			}
			if mode == "restart" {
				h.connection.shutdown(io.EOF)
				app = &replayTestApplication{calls: make(chan int64, 10)}
				h = newConnectionHarnessWithStore(t, now, app, 100, nil, store)
			}
			defer h.connection.shutdown(io.EOF)
			prior := len(h.transport.writtenFrames())
			children := []mtprototl.Message{
				{MsgID: oldID, SeqNo: 1, BodyRaw: constructorBody(0x10101010)},
				{MsgID: freshID, SeqNo: 3, BodyRaw: constructorBody(0x10101010)},
			}
			if mode == "repeated" {
				children = append(children, children[0])
			}
			if mode == "ack_in_container" {
				children = append([]mtprototl.Message{{MsgID: inboundMessageID(12), SeqNo: 2, BodyRaw: encodeControlBody(t, &mtprototl.MsgsAck{MsgIDs: []int64{responseID}})}}, children...)
			}
			send(inboundMessageID(40), 4, serializeInboundContainer(t, children))
			waitCall(freshID)
			extra := 3
			if mode == "completed" || mode == "repeated" || mode == "restart" {
				extra = 4
			}
			waitForWrittenFrames(t, h.transport, prior+extra)
			frames := h.transport.writtenFrames()[prior:]
			resent, retry := false, false
			resendCount := 0
			for _, frame := range frames {
				if original != nil && bytes.Equal(original, frame) {
					resent = true
					resendCount++
				}
				inner := decryptWriterFrame(t, h.authKey, frame)
				if binaryConstructor(inner.Data) == mtprototl.BadMsgNotificationID {
					t.Fatal("valid retransmission rejected")
				}
				if binaryConstructor(inner.Data) == mtprototl.RPCResultID {
					result := &mtprototl.RPCResult{}
					if err := decodeControl(inner.Data, result); err != nil {
						t.Fatal(err)
					}
					if result.ReqMsgID == oldID && binaryConstructor(result.ResultRaw) == mtprototl.RPCErrorID {
						failure := &mtprototl.RPCError{}
						if err := decodeControl(result.ResultRaw, failure); err != nil {
							t.Fatal(err)
						}
						retry = failure.ErrorCode == 500 && failure.ErrorMessage == interruptedRequestRetryMessage
					}
				}
			}
			if (mode == "completed" || mode == "repeated") != resent {
				t.Fatalf("response resend=%v mode=%s", resent, mode)
			}
			if resendCount > 1 {
				t.Fatalf("response replayed %d times in one container", resendCount)
			}
			if (mode == "restart") != retry {
				t.Fatalf("retry=%v mode=%s", retry, mode)
			}
			if mode == "inflight" {
				close(app.release)
				waitForWrittenFrames(t, h.transport, prior+extra+2)
			}
			select {
			case id := <-app.calls:
				t.Fatalf("unexpected duplicate handler %d", id)
			default:
			}
		})
	}
}

func TestSessionValidatorMixedReplayKeepsInvalidContainerAtomic(t *testing.T) {
	original := inboundSnapshot()
	validator := newInboundValidator(t, original)
	accepted, err := validator.Validate(original, &mtproto.InnerData{Salt: inboundSalt, SessionID: inboundSessionID, MsgID: inboundMessageID(4), SeqNo: 1, Data: constructorBody(0x11111111)})
	if err != nil {
		t.Fatal(err)
	}
	original = accepted.Snapshot
	for _, bad := range []mtprototl.Message{
		{MsgID: inboundMessageID(12), SeqNo: 4, BodyRaw: constructorBody(0x11111111)},
		{MsgID: inboundMessageID(12), SeqNo: 4, BodyRaw: constructorBody(mtprototl.MsgContainerID)},
	} {
		body := serializeInboundContainer(t, []mtprototl.Message{
			{MsgID: inboundMessageID(4), SeqNo: 1, BodyRaw: constructorBody(0x11111111)},
			{MsgID: inboundMessageID(8), SeqNo: 3, BodyRaw: constructorBody(0x11111111)}, bad,
		})
		if _, err := validator.Validate(original, &mtproto.InnerData{Salt: inboundSalt, SessionID: inboundSessionID, MsgID: inboundMessageID(20), SeqNo: 4, Data: body}); err == nil {
			t.Fatal("invalid sibling accepted")
		}
		// Neither the outer ID nor fresh child may have consumed durable state.
		if _, err := validator.Validate(original, &mtproto.InnerData{Salt: inboundSalt, SessionID: inboundSessionID, MsgID: inboundMessageID(8), SeqNo: 3, Data: constructorBody(0x11111111)}); err != nil {
			t.Fatalf("fresh child was consumed: %v", err)
		}
	}
}

func TestSessionValidatorDoesNotTreatUnknownOldChildAsRetransmission(t *testing.T) {
	snapshot := inboundSnapshot()
	snapshot.ClientMsgIDFloor = inboundMessageID(4)
	snapshot.LastClientMsgID = inboundMessageID(8)
	snapshot.RecentClientMsgIDs = []int64{inboundMessageID(8)}
	snapshot.SeqNo = 4
	snapshot.RecentClientSeqNos = []int32{3}
	validator := newInboundValidator(t, snapshot)
	body := serializeInboundContainer(t, []mtprototl.Message{
		{MsgID: inboundMessageID(4), SeqNo: 1, BodyRaw: constructorBody(0x11111111)},
		{MsgID: inboundMessageID(12), SeqNo: 5, BodyRaw: constructorBody(0x11111111)},
	})
	if _, err := validator.Validate(snapshot, &mtproto.InnerData{Salt: inboundSalt, SessionID: inboundSessionID, MsgID: inboundMessageID(20), SeqNo: 6, Data: body}); err == nil {
		t.Fatal("unknown child below durable floor was accepted")
	}
}
