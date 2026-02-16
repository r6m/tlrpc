package tlrpc

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
)

var (
	ErrHandshakeFailed    = errors.New("tlrpc: handshake failed")
	ErrInvalidHandshake   = errors.New("tlrpc: invalid handshake message")
	ErrUnsupportedMessage = errors.New("tlrpc: unsupported message type")
)

type HandshakeHandler interface {
	HandleUnencrypted(ctx context.Context, msgID int64, data []byte) ([]byte, error)
}

type DefaultHandshakeHandler struct {
	authKeys crypto.AuthKeyManager
}

func NewDefaultHandshakeHandler(authKeys crypto.AuthKeyManager) *DefaultHandshakeHandler {
	return &DefaultHandshakeHandler{authKeys: authKeys}
}

func (h *DefaultHandshakeHandler) HandleUnencrypted(ctx context.Context, msgID int64, data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, ErrInvalidHandshake
	}

	r := bytes.NewReader(data)
	constructorID, err := mtproto.ReadUint32(r)
	if err != nil {
		return nil, err
	}

	switch constructorID {
	case 0x60469778:
		return h.handleReqPQ(data)
	case 0xd712e2be:
		return h.handlePQInnerData(data)
	case 0xeeb9b9d6:
		return h.handleReqSetClientDHParams(data)
	default:
		return nil, ErrUnsupportedMessage
	}
}

func (h *DefaultHandshakeHandler) handleReqPQ(data []byte) ([]byte, error) {
	r := bytes.NewReader(data)
	_, _ = mtproto.ReadUint32(r)

	nonce, err := mtproto.ReadInt128(r)
	if err != nil {
		return nil, err
	}

	serverNonce, err := generateNonce()
	if err != nil {
		return nil, err
	}

	pq := make([]byte, 8)
	if _, err := rand.Read(pq); err != nil {
		return nil, err
	}

	resp := &bytes.Buffer{}
	if err := mtproto.WriteUint32(resp, 0x5162463f); err != nil {
		return nil, err
	}
	if err := mtproto.WriteInt128(resp, nonce); err != nil {
		return nil, err
	}
	if err := mtproto.WriteInt128(resp, serverNonce); err != nil {
		return nil, err
	}
	if err := mtproto.WriteBytes(resp, pq); err != nil {
		return nil, err
	}
	if err := mtproto.WriteUint64(resp, 1); err != nil {
		return nil, err
	}
	if err := mtproto.WriteVectorHeader(resp, 1); err != nil {
		return nil, err
	}
	if err := mtproto.WriteUint32(resp, 0xc3b6afa0); err != nil {
		return nil, err
	}

	return resp.Bytes(), nil
}

func (h *DefaultHandshakeHandler) handlePQInnerData(data []byte) ([]byte, error) {
	return nil, ErrHandshakeFailed
}

func (h *DefaultHandshakeHandler) handleReqSetClientDHParams(data []byte) ([]byte, error) {
	return nil, ErrHandshakeFailed
}

func generateNonce() ([16]byte, error) {
	var nonce [16]byte
	_, err := rand.Read(nonce[:])
	return nonce, err
}

func (h *connHandler) handleUnencryptedMessage(msg *mtproto.UnencryptedMessage) error {
	if h.server.codec == nil {
		return errors.New("tlrpc: codec is not configured")
	}

	ctx := h.conn.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	var handler HandshakeHandler
	if h.server.codec != nil {
		if hs, ok := h.server.codec.(HandshakeHandler); ok {
			handler = hs
		}
	}
	if handler == nil {
		handler = NewDefaultHandshakeHandler(h.server.authKeys)
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

	if h.server.codec == nil {
		return errors.New("tlrpc: codec is not configured")
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

	req, err := h.server.codec.Decode(layerFromSession(sess), inner.Data)
	if err != nil {
		return err
	}
	methodName := req.Method()
	if methodName == "" {
		return NewNotFoundError("METHOD_NOT_FOUND")
	}
	method, ok := h.server.registry.GetMethod(methodName)
	if !ok {
		return NewNotFoundError("METHOD_NOT_FOUND")
	}

	handler := method.Handler
	var resp interface{}

	// Apply new gRPC-like unary interceptors
	if len(h.server.unaryInterceptors) > 0 {
		info := &UnaryServerInfo{FullMethod: methodName}
		chainedInterceptor := ChainUnaryInterceptors(h.server.unaryInterceptors...)
		resp, err = chainedInterceptor(ctx, req, info, handler)
	} else {
		// Fallback to legacy interceptors
		if len(h.server.legacyInterceptors) > 0 {
			handler = ChainInterceptors(h.server.legacyInterceptors...)(handler)
		}
		resp, err = handler(ctx, req)
	}

	// Handle errors by sending MTProto RPC error
	if err != nil {
		return h.sendRPCError(inner.MsgID, err)
	}

	if resp == nil {
		return nil
	}
	respObj, ok := resp.(TLObject)
	if !ok {
		return h.sendRPCError(inner.MsgID, NewInternalError("response does not implement TLObject"))
	}
	respData, err := h.server.codec.Encode(layerFromSession(sess), respObj)
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
	return h.conn.WriteMessage(serializeEncrypted(encResp))
}

// sendRPCError converts an error to MTProto RPC error format and sends it.
func (h *connHandler) sendRPCError(requestMsgID int64, err error) error {
	// Convert error to RPCError
	rpcErr := FromError(err)

	// Get session info for response
	sess, _ := h.server.sessions.Get(h.authKeyID)

	// Encode the RPC error as TL object
	respData, encErr := h.server.codec.Encode(layerFromSession(sess), rpcErr)
	if encErr != nil {
		// If encoding fails, send a generic internal error
		genericErr := NewInternalError("failed to encode error response")
		respData, _ = h.server.codec.Encode(layerFromSession(sess), genericErr)
	}

	// Send the error response
	innerResp := &mtproto.InnerData{
		Salt:      0, // Use 0 for errors
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
