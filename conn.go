package tlrpc

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
)

type connHandler struct {
	server           *Server
	conn             connIO
	handshakeHandler HandshakeHandler
	authKeyID        crypto.KeyID
}

type connIO interface {
	ReadMessage() ([]byte, error)
	WriteMessage([]byte) error
	Close() error
	Context() context.Context
}

func (h *connHandler) run() error {
	for {
		payload, err := h.conn.ReadMessage()
		if err != nil {
			return err
		}
		if err := h.processMessage(payload); err != nil {
			return err
		}
	}
}

func layerFromSession(sess *session.Session) int {
	if sess == nil {
		return 0
	}
	return sess.Layer
}

func nextMsgID() int64 {
	return time.Now().UnixNano() &^ 3
}

func nextSeqNo(sess *Session) int32 {
	if sess == nil {
		return 0
	}
	sess.SeqNo++
	return sess.SeqNo
}

func serializeEncrypted(msg *mtproto.EncryptedMessage) []byte {
	data := make([]byte, 8+16+len(msg.EncryptedData))
	binary.LittleEndian.PutUint64(data[:8], uint64(msg.AuthKeyID))
	copy(data[8:24], msg.MsgKey[:])
	copy(data[24:], msg.EncryptedData)
	return data
}

// encodeTLObject encodes a TL object using its SerializeTL method
func encodeTLObject(obj TLObject) ([]byte, error) {
	if obj == nil {
		return nil, errors.New("tlrpc: nil object")
	}
	buf := &bytes.Buffer{}
	if err := obj.(interface{ SerializeTL(io.Writer) error }).SerializeTL(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (h *connHandler) handleUnencryptedMessage(msg *mtproto.UnencryptedMessage) error {
	ctx := h.conn.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	handler := h.server.handshakeHandler
	if handler == nil {
		handler = NewDefaultHandshakeHandler(h.server.authKeys, h.server.serverKeys)
	}

	respData, err := handler.HandleUnencrypted(ctx, msg.MsgID, msg.Data)
	if err != nil {
		return err
	}

	respMsg := &mtproto.UnencryptedMessage{
		AuthKeyID: [8]byte{},
		MsgID:     nextMsgID(),
		Data:      respData,
	}

	respBytes, err := respMsg.Serialize()
	if err != nil {
		return err
	}

	return h.conn.WriteMessage(respBytes)
}

func (h *connHandler) processMessage(payload []byte) error {
	if len(payload) < 8 {
		return io.ErrUnexpectedEOF
	}

	keyID := binary.LittleEndian.Uint64(payload[:8])

	if keyID == 0 {
		msg := &mtproto.UnencryptedMessage{}
		if err := msg.Deserialize(payload); err != nil {
			return err
		}
		return h.handleUnencryptedMessage(msg)
	}

	return h.handleEncryptedMessage(payload, crypto.KeyID(keyID))
}

func (h *connHandler) handleEncryptedMessage(payload []byte, keyID crypto.KeyID) error {
	if len(payload) < 24 {
		return io.ErrUnexpectedEOF
	}

	var msgKey [16]byte
	copy(msgKey[:], payload[8:24])
	enc := &mtproto.EncryptedMessage{
		AuthKeyID:     keyID,
		MsgKey:        msgKey,
		EncryptedData: payload[24:],
	}

	authKey, err := h.server.authKeys.Get(keyID)
	if err != nil {
		return ErrUnauthorized
	}
	inner, err := enc.Decrypt(authKey)
	if err != nil {
		return err
	}

	// Validate message ID
	if err := validateMessageID(inner.MsgID); err != nil {
		return err
	}

	// Store the auth key ID for error responses
	h.authKeyID = keyID

	sess, err := h.server.sessions.Get(keyID)
	if err != nil {
		sess, err = h.server.sessions.Create(keyID)
		if err != nil {
			return err
		}
	}
	if sess != nil {
		sess.Touch()
		_ = h.server.sessions.Save(sess)
	}

	ctx := h.conn.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = withSession(ctx, sess)
	ctx = withAuthKeyID(ctx, int64(keyID))
	if sess != nil {
		ctx = withLayer(ctx, sess.Layer)
		ctx = withUserID(ctx, sess.UserID)
	}

	reqObj, _, err := decodeTLObject(h.server.dispatcher, inner.Data)
	if err != nil {
		return h.sendRPCError(inner.MsgID, err)
	}
	ackIDs := []int64{inner.MsgID}
	if container, ok := reqObj.(*mtprototl.MsgContainer); ok {
		for _, msg := range container.Messages {
			ackIDs = append(ackIDs, msg.MsgID)
		}
	}

	respObj, err := h.dispatchDecodedObject(ctx, reqObj)
	if err != nil {
		return h.sendRPCError(inner.MsgID, err)
	}
	if respObj == nil {
		return h.sendAcknowledgment(authKey, keyID, ackIDs...)
	}
	respData, err := encodeTLObject(respObj)
	if err != nil {
		return h.sendRPCError(inner.MsgID, NewInternalError("failed to encode response"))
	}

	innerResp := &mtproto.InnerData{
		Salt:      inner.Salt,
		SessionID: inner.SessionID,
		MsgID:     nextMsgID(),
		SeqNo:     nextSeqNo(sess),
		Data:      respData,
	}
	encResp, err := innerResp.Encrypt(authKey, keyID)
	if err != nil {
		return err
	}
	if err := h.conn.WriteMessage(serializeEncrypted(encResp)); err != nil {
		return err
	}

	// Send acknowledgment for the received message(s).
	return h.sendAcknowledgment(authKey, keyID, ackIDs...)
}

func (h *connHandler) dispatchDecodedObject(ctx context.Context, req TLObject) (TLObject, error) {
	switch obj := req.(type) {
	case *mtprototl.MsgContainer:
		return h.dispatchContainer(ctx, obj)
	case *mtprototl.MsgsStateReq, *mtprototl.MsgResendReq, *mtprototl.MsgsAck:
		return nil, nil
	case *mtprototl.GzipPacked:
		gr, err := gzip.NewReader(bytes.NewReader(obj.PackedData))
		if err != nil {
			return nil, err
		}
		defer gr.Close()
		unpacked, err := io.ReadAll(gr)
		if err != nil {
			return nil, err
		}
		inner, _, err := decodeTLObject(h.server.dispatcher, unpacked)
		if err != nil {
			return nil, err
		}
		return h.dispatchDecodedObject(ctx, inner)
	case *mtprototl.RPCResult:
		if len(obj.ResultRaw) == 0 {
			return nil, nil
		}
		inner, _, err := decodeTLObject(h.server.dispatcher, obj.ResultRaw)
		if err != nil {
			return nil, err
		}
		return h.dispatchDecodedObject(ctx, inner)
	default:
		return h.invokeMethod(ctx, req)
	}
}

func (h *connHandler) invokeMethod(ctx context.Context, req TLObject) (TLObject, error) {
	constructorID := req.ConstructorID()
	methodHandler, ok := h.server.dispatcher.LookupMethod(constructorID)
	if !ok {
		return nil, NewNotFoundError("METHOD_NOT_FOUND")
	}

	var (
		resp interface{}
		err  error
	)
	if len(h.server.unaryInterceptors) > 0 {
		info := &UnaryServerInfo{FullMethod: fmt.Sprintf("constructor_%08x", constructorID)}
		chainedInterceptor := ChainUnaryInterceptors(h.server.unaryInterceptors...)
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			return methodHandler(ctx, req.(TLObject))
		}
		resp, err = chainedInterceptor(ctx, req, info, handler)
	} else {
		resp, err = methodHandler(ctx, req)
	}
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, nil
	}
	respObj, ok := resp.(TLObject)
	if !ok {
		return nil, NewInternalError("response does not implement TLObject")
	}
	return respObj, nil
}

func (h *connHandler) dispatchContainer(ctx context.Context, container *mtprototl.MsgContainer) (TLObject, error) {
	var responses []mtprototl.Message
	for _, msg := range container.Messages {
		if len(msg.BodyRaw) < 4 {
			continue
		}
		obj, _, err := decodeTLObject(h.server.dispatcher, msg.BodyRaw)
		if err != nil {
			continue
		}
		respObj, err := h.dispatchDecodedObject(ctx, obj)
		if err != nil || respObj == nil {
			continue
		}
		respBytes, err := encodeTLObject(respObj)
		if err != nil {
			continue
		}
		responses = append(responses, mtprototl.Message{
			MsgID:   nextMsgID(),
			SeqNo:   msg.SeqNo,
			BodyRaw: respBytes,
		})
	}

	switch len(responses) {
	case 0:
		return nil, nil
	case 1:
		respObj, _, err := decodeTLObject(h.server.dispatcher, responses[0].BodyRaw)
		if err != nil {
			return nil, err
		}
		return respObj, nil
	default:
		return &mtprototl.MsgContainer{Messages: responses}, nil
	}
}

// sendAcknowledgment sends an acknowledgment for received message IDs
func (h *connHandler) sendAcknowledgment(authKey crypto.AuthKey, keyID crypto.KeyID, msgIDs ...int64) error {
	if len(msgIDs) == 0 {
		return nil
	}

	ack := &mtprototl.MsgsAck{MsgIDs: msgIDs}
	ackData, err := encodeTLObject(ack)
	if err != nil {
		return err
	}

	innerAck := &mtproto.InnerData{
		Salt:      0, // Use 0 for acks
		SessionID: 0, // Use 0 for acks
		MsgID:     nextMsgID(),
		SeqNo:     0, // Acks don't need sequence numbers
		Data:      ackData,
	}

	encAck, err := innerAck.Encrypt(authKey, keyID)
	if err != nil {
		return err
	}

	return h.conn.WriteMessage(serializeEncrypted(encAck))
}

// sendRPCError converts an error to MTProto RPC error format and sends it.
func (h *connHandler) sendRPCError(requestMsgID int64, err error) error {
	// Convert error to MTProto rpc_error
	rpcErr := FromError(err)
	mtErr := &mtprototl.RPCError{
		ErrorCode:    rpcErr.ErrorCode,
		ErrorMessage: rpcErr.ErrorMessage,
	}

	respData, encErr := encodeTLObject(mtErr)
	if encErr != nil {
		// If encoding fails, send a generic internal error
		fallback := &mtprototl.RPCError{
			ErrorCode:    int32(Internal),
			ErrorMessage: "failed to encode error response",
		}
		respData, _ = encodeTLObject(fallback)
	}

	// Send the error response
	innerResp := &mtproto.InnerData{
		Salt:      0,                 // Use 0 for errors
		SessionID: requestMsgID &^ 3, // Use request msg_id as session_id for errors
		MsgID:     nextMsgID(),
		SeqNo:     0, // Errors don't need sequence numbers
		Data:      respData,
	}

	authKey, keyErr := h.server.authKeys.Get(h.authKeyID)
	if keyErr != nil {
		return keyErr
	}

	encResp, encErr := innerResp.Encrypt(authKey, h.authKeyID)
	if encErr != nil {
		return encErr
	}

	return h.conn.WriteMessage(serializeEncrypted(encResp))
}
