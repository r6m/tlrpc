package runtime

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/mtproto/reliability"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
)

func TestWriterOwnsOrderingSequencePersistenceAndRetention(t *testing.T) {
	h := newWriterHarness(t, session.NewMemoryStore(), &recordingFrameSink{})
	pushBody := constructorBody(0x01020304)
	resultBody := constructorBody(0x05060708)

	if err := h.writer.Submit(context.Background(), Push{Body: pushBody}); err != nil {
		t.Fatalf("submit push: %v", err)
	}
	if err := h.writer.Submit(context.Background(), RPCResult{RequestMessageID: 77, Body: resultBody}); err != nil {
		t.Fatalf("submit RPC result: %v", err)
	}

	frames := h.sink.snapshot()
	if len(frames) != 2 {
		t.Fatalf("written frames = %d, want 2", len(frames))
	}
	first := decryptWriterFrame(t, h.authKey, frames[0])
	second := decryptWriterFrame(t, h.authKey, frames[1])
	if first.MsgID >= second.MsgID || first.MsgID&3 != 3 || second.MsgID&3 != 1 {
		t.Fatalf("message IDs = %d, %d; want increasing push(3), response(1)", first.MsgID, second.MsgID)
	}
	if first.SeqNo != 1 || second.SeqNo != 3 {
		t.Fatalf("sequence numbers = %d, %d; want 1, 3", first.SeqNo, second.SeqNo)
	}
	if got := binary.LittleEndian.Uint32(first.Data[:4]); got != 0x01020304 {
		t.Fatalf("push constructor = 0x%08x", got)
	}
	result := &mtprototl.RPCResult{}
	if err := result.DeserializeTL(bytes.NewReader(second.Data)); err != nil {
		t.Fatalf("decode RPC result: %v", err)
	}
	if result.ReqMsgID != 77 || !bytes.Equal(result.ResultRaw, resultBody) {
		t.Fatalf("RPC result = req:%d body:%x", result.ReqMsgID, result.ResultRaw)
	}

	stored, err := h.store.Load(context.Background(), h.key)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if stored.ServerSeqNo != 2 {
		t.Fatalf("persisted server sequence = %d, want 2", stored.ServerSeqNo)
	}
	for _, inner := range []*mtproto.InnerData{first, second} {
		retained, ok := h.reliability.LookupForResend(inner.MsgID)
		if !ok || !bytes.Equal(retained.Payload, frames[0]) && !bytes.Equal(retained.Payload, frames[1]) {
			t.Fatalf("message %d was not retained", inner.MsgID)
		}
	}
}

func TestWriterBuildsContainerChildrenInsideOneSequenceBoundary(t *testing.T) {
	h := newWriterHarness(t, session.NewMemoryStore(), &recordingFrameSink{})
	if err := h.writer.Submit(context.Background(), Batch{Items: []Intent{
		Push{Body: constructorBody(0x01020304)},
		RPCResult{RequestMessageID: 88, Body: constructorBody(0x05060708)},
	}}); err != nil {
		t.Fatalf("submit batch: %v", err)
	}
	frames := h.sink.snapshot()
	if len(frames) != 1 {
		t.Fatalf("written frames = %d, want 1 container", len(frames))
	}
	outer := decryptWriterFrame(t, h.authKey, frames[0])
	if outer.SeqNo != 4 || outer.MsgID&3 != 1 {
		t.Fatalf("outer id/seq = %d/%d, want response bits and seq 4", outer.MsgID, outer.SeqNo)
	}
	container := &mtprototl.MsgContainer{}
	if err := container.DeserializeTL(bytes.NewReader(outer.Data)); err != nil {
		t.Fatalf("decode container: %v", err)
	}
	if len(container.Messages) != 2 {
		t.Fatalf("children = %d, want 2", len(container.Messages))
	}
	first, second := container.Messages[0], container.Messages[1]
	if first.SeqNo != 1 || second.SeqNo != 3 || first.MsgID >= second.MsgID || second.MsgID >= outer.MsgID {
		t.Fatalf("child/outer order = (%d,%d) (%d,%d) outer %d", first.MsgID, first.SeqNo, second.MsgID, second.SeqNo, outer.MsgID)
	}
	for _, message := range []mtprototl.Message{first, second} {
		retained, ok := h.reliability.LookupForResend(message.MsgID)
		if !ok {
			t.Fatalf("container child %d is unknown to reliability state", message.MsgID)
		}
		if retained.SequenceNumber != message.SeqNo || !bytes.Equal(retained.Payload, frames[0]) {
			t.Fatalf("container child %d retained wrong frame metadata", message.MsgID)
		}
	}
}

func TestWriterPersistenceFailureWritesNothingAndRetiresLease(t *testing.T) {
	wantErr := errors.New("save failed")
	store := &failingSaveStore{Store: session.NewMemoryStore(), err: wantErr}
	h := newWriterHarness(t, store, &recordingFrameSink{})
	err := h.writer.Submit(context.Background(), Push{Body: constructorBody(0x01020304)})
	if !errors.Is(err, wantErr) {
		t.Fatalf("submit error = %v, want save failure", err)
	}
	if len(h.sink.snapshot()) != 0 {
		t.Fatal("writer emitted a frame after persistence failure")
	}
	if !errors.Is(context.Cause(h.lease.Context()), wantErr) {
		t.Fatalf("lease cause = %v, want save failure", context.Cause(h.lease.Context()))
	}
}

func TestWriterAmbiguousWriteFailureRetainsPacketAndRetiresLease(t *testing.T) {
	wantErr := errors.New("write failed")
	sink := &recordingFrameSink{writeErr: wantErr}
	h := newWriterHarness(t, session.NewMemoryStore(), sink)
	err := h.writer.Submit(context.Background(), Push{Body: constructorBody(0x01020304)})
	if !errors.Is(err, wantErr) {
		t.Fatalf("submit error = %v, want write failure", err)
	}
	stored, loadErr := h.store.Load(context.Background(), h.key)
	if loadErr != nil {
		t.Fatalf("load session: %v", loadErr)
	}
	if stored.ServerSeqNo != 1 {
		t.Fatalf("persisted server sequence = %d, want reserved value 1", stored.ServerSeqNo)
	}
	if h.reliability.Len() != 1 {
		t.Fatalf("retained messages = %d, want 1 ambiguous packet", h.reliability.Len())
	}
	if !errors.Is(context.Cause(h.lease.Context()), wantErr) {
		t.Fatalf("lease cause = %v, want write failure", context.Cause(h.lease.Context()))
	}
}

func TestWriterRejectsOversizedResponseBeforePersistenceOrEncryption(t *testing.T) {
	h := newWriterHarnessWithMaxEncodedBytes(t, session.NewMemoryStore(), &recordingFrameSink{}, 32)
	err := h.writer.Submit(context.Background(), Push{Body: make([]byte, 33)})
	if !errors.Is(err, ErrEncodedPayloadTooLarge) {
		t.Fatalf("submit error = %v, want ErrEncodedPayloadTooLarge", err)
	}
	if got := len(h.sink.snapshot()); got != 0 {
		t.Fatalf("written frames = %d, want 0", got)
	}
	if h.reliability.Len() != 0 {
		t.Fatalf("retained messages = %d, want 0", h.reliability.Len())
	}
	stored, loadErr := h.store.Load(context.Background(), h.key)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if stored.ServerSeqNo != 0 {
		t.Fatalf("persisted server sequence = %d, want 0", stored.ServerSeqNo)
	}
}

func TestWriterSerializesAcknowledgementAndReliabilityInspectionWithoutWrites(t *testing.T) {
	h := newWriterHarness(t, session.NewMemoryStore(), &recordingFrameSink{})
	if err := h.writer.Submit(context.Background(), Push{Body: constructorBody(0x01020304)}); err != nil {
		t.Fatalf("submit push: %v", err)
	}
	frames := h.sink.snapshot()
	if len(frames) != 1 {
		t.Fatalf("written frames = %d, want 1", len(frames))
	}
	messageID := decryptWriterFrame(t, h.authKey, frames[0]).MsgID

	state, err := h.writer.InspectOutbound(context.Background(), messageID)
	if err != nil {
		t.Fatalf("inspect retained message: %v", err)
	}
	if state != (OutboundReliabilityState{Known: true, ResendEligible: true}) {
		t.Fatalf("initial reliability state = %+v", state)
	}

	if err := h.writer.AcknowledgeOutbound(context.Background(), []int64{messageID, messageID, 999999}); err != nil {
		t.Fatalf("acknowledge retained messages: %v", err)
	}
	state, err = h.writer.InspectOutbound(context.Background(), messageID)
	if err != nil {
		t.Fatalf("inspect acknowledged message: %v", err)
	}
	if state != (OutboundReliabilityState{Known: true, Acknowledged: true}) {
		t.Fatalf("acknowledged reliability state = %+v", state)
	}
	unknown, err := h.writer.InspectOutbound(context.Background(), 999999)
	if err != nil {
		t.Fatalf("inspect unknown message: %v", err)
	}
	if unknown != (OutboundReliabilityState{}) {
		t.Fatalf("unknown reliability state = %+v, want zero value", unknown)
	}
	if got := len(h.sink.snapshot()); got != 1 {
		t.Fatalf("written frames after inspect/ack = %d, want 1", got)
	}
}

func TestWriterReliabilityCommandsHonorCancellationWithoutMutation(t *testing.T) {
	h := newWriterHarness(t, session.NewMemoryStore(), &recordingFrameSink{})
	if err := h.writer.Submit(context.Background(), Push{Body: constructorBody(0x01020304)}); err != nil {
		t.Fatalf("submit push: %v", err)
	}
	frames := h.sink.snapshot()
	messageID := decryptWriterFrame(t, h.authKey, frames[0]).MsgID

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.writer.AcknowledgeOutbound(canceled, []int64{messageID}); !errors.Is(err, context.Canceled) {
		t.Fatalf("acknowledge error = %v, want context cancellation", err)
	}
	if _, err := h.writer.InspectOutbound(canceled, messageID); !errors.Is(err, context.Canceled) {
		t.Fatalf("inspect error = %v, want context cancellation", err)
	}

	state, err := h.writer.InspectOutbound(context.Background(), messageID)
	if err != nil {
		t.Fatalf("inspect after canceled commands: %v", err)
	}
	if state != (OutboundReliabilityState{Known: true, ResendEligible: true}) {
		t.Fatalf("state after canceled commands = %+v", state)
	}
	if got := len(h.sink.snapshot()); got != 1 {
		t.Fatalf("written frames after canceled commands = %d, want 1", got)
	}
}

func TestWriterReliabilityCommandsReturnShutdownCause(t *testing.T) {
	h := newWriterHarness(t, session.NewMemoryStore(), &recordingFrameSink{})
	wantErr := errors.New("writer stopped")
	if err := h.writer.Submit(context.Background(), Close{Cause: wantErr}); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	select {
	case <-h.writer.Done():
	case <-time.After(time.Second):
		t.Fatal("writer did not stop")
	}

	if err := h.writer.AcknowledgeOutbound(context.Background(), []int64{1}); !errors.Is(err, wantErr) {
		t.Fatalf("acknowledge error = %v, want shutdown cause", err)
	}
	if _, err := h.writer.InspectOutbound(context.Background(), 1); !errors.Is(err, wantErr) {
		t.Fatalf("inspect error = %v, want shutdown cause", err)
	}
	if got := len(h.sink.snapshot()); got != 0 {
		t.Fatalf("written frames = %d, want 0", got)
	}
}

type writerHarness struct {
	writer      *Writer
	lease       *SessionLease
	store       session.Store
	key         session.SessionKey
	authKey     crypto.AuthKey
	sink        *recordingFrameSink
	reliability *reliability.Store
}

func newWriterHarness(t *testing.T, store session.Store, sink *recordingFrameSink) *writerHarness {
	return newWriterHarnessWithMaxEncodedBytes(t, store, sink, 0)
}

func newWriterHarnessWithMaxEncodedBytes(t *testing.T, store session.Store, sink *recordingFrameSink, maxEncodedBytes int) *writerHarness {
	t.Helper()
	var authKey crypto.AuthKey
	for index := range authKey {
		authKey[index] = byte(index + 1)
	}
	key := session.SessionKey{AuthKeyID: authKey.ID(), SessionID: 99}
	initial := session.Snapshot{AuthKeyID: key.AuthKeyID, SessionID: key.SessionID, ServerSalt: 101}
	registry := NewSessionLeaseRegistry(store)
	lease, err := registry.Acquire(context.Background(), key, initial)
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	now := time.Unix(1000, 0).UTC()
	retained, err := reliability.New(reliability.Config{Capacity: 16, TTL: time.Hour, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("new reliability store: %v", err)
	}
	writer, err := NewWriter(context.Background(), WriterConfig{
		Lease:           lease,
		AuthKey:         authKey,
		Sink:            sink,
		MessageIDs:      &fixedMessageIDs{next: 100},
		Reliability:     retained,
		Now:             func() time.Time { return now },
		MaxEncodedBytes: maxEncodedBytes,
	})
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	h := &writerHarness{writer: writer, lease: lease, store: store, key: key, authKey: authKey, sink: sink, reliability: retained}
	t.Cleanup(func() {
		_ = writer.Submit(context.Background(), Close{Cause: ErrWriterClosed})
		select {
		case <-writer.Done():
		case <-time.After(time.Second):
			t.Error("writer did not stop")
		}
		lease.Release()
	})
	return h
}

type fixedMessageIDs struct {
	next int64
}

func (s *fixedMessageIDs) Next() int64 {
	value := s.next
	s.next += 4
	return value
}

type recordingFrameSink struct {
	mu       sync.Mutex
	frames   [][]byte
	writeErr error
	closed   bool
}

func (s *recordingFrameSink) WriteFrame(_ context.Context, frame []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writeErr != nil {
		return s.writeErr
	}
	s.frames = append(s.frames, append([]byte(nil), frame...))
	return nil
}

func (s *recordingFrameSink) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

func (s *recordingFrameSink) snapshot() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	frames := make([][]byte, len(s.frames))
	for index := range s.frames {
		frames[index] = append([]byte(nil), s.frames[index]...)
	}
	return frames
}

type failingSaveStore struct {
	session.Store
	err error
}

func (s *failingSaveStore) Save(context.Context, session.SessionKey, session.Snapshot) error {
	return s.err
}

func constructorBody(constructorID uint32) []byte {
	body := make([]byte, 4)
	binary.LittleEndian.PutUint32(body, constructorID)
	return body
}

func decryptWriterFrame(t *testing.T, authKey crypto.AuthKey, frame []byte) *mtproto.InnerData {
	t.Helper()
	if len(frame) < 24 {
		t.Fatalf("encrypted frame is truncated: %d bytes", len(frame))
	}
	message := &mtproto.EncryptedMessage{AuthKeyID: crypto.KeyID(binary.LittleEndian.Uint64(frame[:8]))}
	copy(message.MsgKey[:], frame[8:24])
	message.EncryptedData = append([]byte(nil), frame[24:]...)
	inner, err := message.Decrypt(authKey)
	if err != nil {
		t.Fatalf("decrypt writer frame: %v", err)
	}
	return inner
}
