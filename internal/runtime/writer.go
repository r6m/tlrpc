package runtime

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/mtproto/reliability"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
)

var (
	ErrWriterClosed            = errors.New("runtime: writer closed")
	ErrRetainedMessageNotFound = errors.New("runtime: retained message not found for resend")
)

type FrameSink interface {
	WriteFrame(ctx context.Context, frame []byte) error
	Close() error
}

type MessageIDSource interface {
	Next() int64
}

type WriterConfig struct {
	Lease       *SessionLease
	AuthKey     crypto.AuthKey
	Sink        FrameSink
	MessageIDs  MessageIDSource
	Reliability *reliability.Store
	Now         func() time.Time
}

// Writer is the only Runtime v2 component allowed to allocate outbound wire
// state or write encrypted frames for a connection.
type Writer struct {
	lease       *SessionLease
	authKey     crypto.AuthKey
	sink        FrameSink
	messageIDs  MessageIDSource
	reliability *reliability.Store
	now         func() time.Time
	ctx         context.Context
	cancel      context.CancelCauseFunc
	requests    chan writerRequest
	done        chan struct{}
	closeOnce   sync.Once
}

// OutboundReliabilityState is a point-in-time view of one retained outbound
// message. Unknown and expired message IDs have all fields set to false.
type OutboundReliabilityState struct {
	Known          bool
	Acknowledged   bool
	ResendEligible bool
}

type writerOperation uint8

const (
	writerOperationSubmit writerOperation = iota
	writerOperationAcknowledge
	writerOperationInspect
)

type writerRequest struct {
	ctx        context.Context
	operation  writerOperation
	intent     Intent
	messageIDs []int64
	messageID  int64
	reply      chan writerResponse
}

type writerResponse struct {
	state OutboundReliabilityState
	err   error
}

func NewWriter(parent context.Context, config WriterConfig) (*Writer, error) {
	if config.Lease == nil || config.Sink == nil || config.Reliability == nil {
		return nil, errors.New("runtime: incomplete writer configuration")
	}
	if parent == nil {
		parent = context.Background()
	}
	messageIDs := config.MessageIDs
	if messageIDs == nil {
		messageIDs = mtproto.NewMsgIDGenerator()
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	ctx, cancel := context.WithCancelCause(parent)
	w := &Writer{
		lease:       config.Lease,
		authKey:     config.AuthKey,
		sink:        config.Sink,
		messageIDs:  messageIDs,
		reliability: config.Reliability,
		now:         now,
		ctx:         ctx,
		cancel:      cancel,
		requests:    make(chan writerRequest),
		done:        make(chan struct{}),
	}
	go w.run()
	return w, nil
}

func (w *Writer) Submit(ctx context.Context, intent Intent) error {
	if err := ValidateIntent(intent); err != nil {
		return err
	}
	response := w.request(ctx, writerRequest{operation: writerOperationSubmit, intent: intent})
	return response.err
}

// AcknowledgeOutbound marks retained outbound message IDs as acknowledged.
// Unknown and expired IDs are ignored so repeated or stale peer
// acknowledgements remain idempotent.
func (w *Writer) AcknowledgeOutbound(ctx context.Context, messageIDs []int64) error {
	response := w.request(ctx, writerRequest{
		operation:  writerOperationAcknowledge,
		messageIDs: append([]int64(nil), messageIDs...),
	})
	return response.err
}

// InspectOutbound returns the writer-owned reliability state for one outbound
// message without exposing the underlying reliability store.
func (w *Writer) InspectOutbound(ctx context.Context, messageID int64) (OutboundReliabilityState, error) {
	response := w.request(ctx, writerRequest{operation: writerOperationInspect, messageID: messageID})
	return response.state, response.err
}

func (w *Writer) request(ctx context.Context, request writerRequest) writerResponse {
	if ctx == nil {
		ctx = context.Background()
	}
	request.ctx = ctx
	request.reply = make(chan writerResponse, 1)
	select {
	case w.requests <- request:
	case <-ctx.Done():
		return writerResponse{err: ctx.Err()}
	case <-w.done:
		return writerResponse{err: w.cause()}
	}
	select {
	case response := <-request.reply:
		return response
	case <-ctx.Done():
		return writerResponse{err: ctx.Err()}
	case <-w.done:
		select {
		case response := <-request.reply:
			return response
		default:
			return writerResponse{err: w.cause()}
		}
	}
}

func (w *Writer) Done() <-chan struct{}    { return w.done }
func (w *Writer) Context() context.Context { return w.ctx }

func (w *Writer) cause() error {
	if cause := context.Cause(w.ctx); cause != nil {
		return cause
	}
	return ErrWriterClosed
}

func (w *Writer) run() {
	defer func() {
		_ = w.sink.Close()
		w.closeOnce.Do(func() { close(w.done) })
	}()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-w.lease.Context().Done():
			w.cancel(context.Cause(w.lease.Context()))
			return
		case request := <-w.requests:
			response, stop, fatal := w.processRequest(request)
			request.reply <- response
			if fatal != nil {
				w.lease.Retire(fatal)
				w.cancel(fatal)
				return
			}
			if stop {
				cause := request.intent.(Close).Cause
				w.lease.Retire(cause)
				w.cancel(cause)
				return
			}
		}
	}
}

func (w *Writer) processRequest(request writerRequest) (writerResponse, bool, error) {
	switch request.operation {
	case writerOperationAcknowledge:
		if err := request.ctx.Err(); err != nil {
			return writerResponse{err: err}, false, nil
		}
		for _, messageID := range request.messageIDs {
			w.reliability.Acknowledge(messageID)
		}
		return writerResponse{}, false, nil
	case writerOperationInspect:
		if err := request.ctx.Err(); err != nil {
			return writerResponse{err: err}, false, nil
		}
		message, known := w.reliability.Lookup(request.messageID)
		return writerResponse{state: OutboundReliabilityState{
			Known:          known,
			Acknowledged:   known && message.Acknowledged,
			ResendEligible: known && !message.Acknowledged,
		}}, false, nil
	case writerOperationSubmit:
		stop, err := w.process(request.ctx, request.intent)
		return writerResponse{err: err}, stop, err
	default:
		err := errors.New("runtime: unknown writer operation")
		return writerResponse{err: err}, false, err
	}
}

func (w *Writer) process(ctx context.Context, intent Intent) (bool, error) {
	switch value := intent.(type) {
	case Close:
		return true, nil
	case Resend:
		for _, messageID := range value.MessageIDs {
			retained, ok := w.reliability.LookupForResend(messageID)
			if !ok {
				return false, fmt.Errorf("%w: %d", ErrRetainedMessageNotFound, messageID)
			}
			if err := w.sink.WriteFrame(ctx, retained.Payload); err != nil {
				return false, err
			}
		}
		return false, nil
	default:
		prepared, next, err := w.prepare(intent)
		if err != nil {
			return false, err
		}
		if err := w.lease.Commit(ctx, next); err != nil {
			return false, err
		}
		if _, err := w.reliability.Put(reliability.SentMessage{
			MessageID:      prepared.messageID,
			SequenceNumber: prepared.sequenceNo,
			Payload:        prepared.frame,
			SentAt:         w.now(),
		}); err != nil {
			return false, err
		}
		if err := w.sink.WriteFrame(ctx, prepared.frame); err != nil {
			return false, err
		}
		return false, nil
	}
}

type preparedFrame struct {
	messageID  int64
	sequenceNo int32
	frame      []byte
}

func (w *Writer) prepare(intent Intent) (preparedFrame, session.Snapshot, error) {
	snapshot, err := w.lease.Snapshot()
	if err != nil {
		return preparedFrame{}, session.Snapshot{}, err
	}

	var body []byte
	var messageID int64
	var sequenceNo int32
	if batch, ok := intent.(Batch); ok {
		container := &mtprototl.MsgContainer{Messages: make([]mtprototl.Message, 0, len(batch.Items))}
		for _, child := range batch.Items {
			childBody, contentRelated, push, err := encodeIntentBody(child)
			if err != nil {
				return preparedFrame{}, session.Snapshot{}, err
			}
			childMessageID := w.nextMessageID(push)
			childSequenceNo := nextSequenceNo(&snapshot, contentRelated)
			container.Messages = append(container.Messages, mtprototl.Message{
				MsgID:   childMessageID,
				SeqNo:   childSequenceNo,
				Bytes:   int32(len(childBody)),
				BodyRaw: childBody,
			})
		}
		body, err = serializeRuntimeTL(container)
		if err != nil {
			return preparedFrame{}, session.Snapshot{}, err
		}
		messageID = w.nextMessageID(false)
		sequenceNo = nextSequenceNo(&snapshot, false)
	} else {
		var contentRelated, push bool
		body, contentRelated, push, err = encodeIntentBody(intent)
		if err != nil {
			return preparedFrame{}, session.Snapshot{}, err
		}
		messageID = w.nextMessageID(push)
		sequenceNo = nextSequenceNo(&snapshot, contentRelated)
	}

	inner := &mtproto.InnerData{
		Salt:      snapshot.ServerSalt,
		SessionID: snapshot.SessionID,
		MsgID:     messageID,
		SeqNo:     sequenceNo,
		Data:      body,
	}
	encrypted, err := inner.Encrypt(w.authKey, snapshot.AuthKeyID)
	if err != nil {
		return preparedFrame{}, session.Snapshot{}, err
	}
	snapshot.LastActivity = w.now().UTC()
	return preparedFrame{
		messageID:  messageID,
		sequenceNo: sequenceNo,
		frame:      serializeEncryptedFrame(encrypted),
	}, snapshot, nil
}

func encodeIntentBody(intent Intent) (body []byte, contentRelated, push bool, err error) {
	switch value := intent.(type) {
	case RPCResult:
		body, err = serializeRuntimeTL(&mtprototl.RPCResult{ReqMsgID: value.RequestMessageID, ResultRaw: value.Body})
		return body, true, false, err
	case RPCError:
		errorBody, encodeErr := serializeRuntimeTL(&mtprototl.RPCError{ErrorCode: value.Code, ErrorMessage: value.Message})
		if encodeErr != nil {
			return nil, false, false, encodeErr
		}
		body, err = serializeRuntimeTL(&mtprototl.RPCResult{ReqMsgID: value.RequestMessageID, ResultRaw: errorBody})
		return body, true, false, err
	case ProtocolReply:
		return append([]byte(nil), value.Body...), value.ContentRelated, value.Unsolicited, nil
	case Acknowledge:
		body, err = serializeRuntimeTL(&mtprototl.MsgsAck{MsgIDs: append([]int64(nil), value.MessageIDs...)})
		return body, false, false, err
	case Push:
		return append([]byte(nil), value.Body...), true, true, nil
	default:
		return nil, false, false, fmt.Errorf("%w: writer body %T", ErrInvalidIntent, intent)
	}
}

func (w *Writer) nextMessageID(push bool) int64 {
	base := w.messageIDs.Next() &^ int64(3)
	if push {
		return base | 3
	}
	return base | 1
}

func nextSequenceNo(snapshot *session.Snapshot, contentRelated bool) int32 {
	if contentRelated {
		sequenceNo := snapshot.ServerSeqNo*2 + 1
		snapshot.ServerSeqNo++
		return sequenceNo
	}
	return snapshot.ServerSeqNo * 2
}

func serializeRuntimeTL(value interface{ SerializeTL(io.Writer) error }) ([]byte, error) {
	var buffer bytes.Buffer
	if err := value.SerializeTL(&buffer); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func serializeEncryptedFrame(message *mtproto.EncryptedMessage) []byte {
	frame := make([]byte, 8+16+len(message.EncryptedData))
	binary.LittleEndian.PutUint64(frame[:8], uint64(message.AuthKeyID))
	copy(frame[8:24], message.MsgKey[:])
	copy(frame[24:], message.EncryptedData)
	return frame
}
