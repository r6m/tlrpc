package tlrpc

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
)

type connHandler struct {
	server         *Server
	conn           connIO
	authKeyID      crypto.KeyID
	disableUpdates bool
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
		if sess.ServerSalt == 0 {
			sess.ServerSalt = time.Now().UTC().UnixNano()
		}
		sess.Touch()
	}
	if sess == nil {
		return NewInternalError("missing session")
	}

	if err := validateMessageID(inner.MsgID); err != nil {
		return h.sendBadMsgNotification(authKey, keyID, inner.MsgID, inner.SeqNo, 16)
	}
	if sess != nil && sess.LastClientMsgID != 0 && inner.MsgID <= sess.LastClientMsgID {
		return h.sendBadMsgNotification(authKey, keyID, inner.MsgID, inner.SeqNo, 32)
	}
	if sess != nil {
		if sess.SessionID == 0 {
			sess.SessionID = inner.SessionID
		} else if inner.SessionID != 0 && inner.SessionID != sess.SessionID {
			return h.sendBadMsgNotification(authKey, keyID, inner.MsgID, inner.SeqNo, 64)
		}
		if inner.Salt != sess.ServerSalt {
			return h.sendBadServerSalt(authKey, keyID, inner.MsgID, inner.SeqNo, sess.ServerSalt)
		}
		sess.LastClientMsgID = inner.MsgID
		_ = h.server.sessions.Save(sess)
	}

	ctx := h.conn.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = withSession(ctx, sess)
	ctx = withAuthKeyID(ctx, int64(keyID))
	if conn, ok := h.conn.(Conn); ok {
		ctx = withConn(ctx, conn)
	}
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

	respObj, err := h.dispatchDecodedObject(ctx, reqObj, inner.MsgID)
	if err != nil {
		return h.sendRPCError(inner.MsgID, err)
	}
	if sess != nil && sess.UserID != 0 && h.server.updateHub != nil && !h.disableUpdates {
		h.server.updateHub.bind(sess.UserID, updateBinding{
			conn:      h.conn,
			keyID:     keyID,
			sessionID: sess.SessionID,
			salt:      sess.ServerSalt,
		})
	}
	if respObj == nil {
		return h.sendAcknowledgment(authKey, keyID, ackIDs...)
	}
	resultData, err := encodeTLObject(respObj)
	if err != nil {
		return h.sendRPCError(inner.MsgID, NewInternalError("failed to encode response"))
	}
	rpcResult := &mtprototl.RPCResult{
		ReqMsgID:  inner.MsgID,
		ResultRaw: resultData,
	}
	respData, err := encodeTLObject(rpcResult)
	if err != nil {
		return h.sendRPCError(inner.MsgID, NewInternalError("failed to encode rpc_result"))
	}

	innerResp := &mtproto.InnerData{
		Salt:      sess.ServerSalt,
		SessionID: sess.SessionID,
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

func (h *connHandler) dispatchDecodedObject(ctx context.Context, req TLObject, requestMsgID int64) (TLObject, error) {
	switch obj := req.(type) {
	case *mtprototl.MsgContainer:
		return h.dispatchContainer(ctx, obj, requestMsgID)
	case *mtprototl.MsgsStateReq, *mtprototl.MsgResendReq:
		return &mtprototl.MsgsStateInfo{ReqMsgID: requestMsgID, Info: []byte{}}, nil
	case *mtprototl.MsgsAck:
		return nil, nil
	case *mtprototl.GzipPacked:
		gr, err := gzip.NewReader(bytes.NewReader(obj.PackedData))
		if err != nil {
			return nil, err
		}
		defer func() { _ = gr.Close() }()
		unpacked, err := io.ReadAll(gr)
		if err != nil {
			return nil, err
		}
		inner, _, err := decodeTLObject(h.server.dispatcher, unpacked)
		if err != nil {
			return nil, err
		}
		return h.dispatchDecodedObject(ctx, inner, requestMsgID)
	case *mtprototl.RPCResult:
		if len(obj.ResultRaw) == 0 {
			return nil, nil
		}
		inner, _, err := decodeTLObject(h.server.dispatcher, obj.ResultRaw)
		if err != nil {
			return nil, err
		}
		return h.dispatchDecodedObject(ctx, inner, requestMsgID)
	case *mtprototl.InvokeWithLayer:
		return h.dispatchInvokeWithLayer(ctx, obj, requestMsgID)
	case *mtprototl.InitConnection:
		return h.dispatchInitConnection(ctx, obj, requestMsgID)
	case *mtprototl.InvokeAfterMsg:
		return h.dispatchWrappedQuery(ctx, obj.QueryRaw, requestMsgID)
	case *mtprototl.InvokeAfterMsgs:
		return h.dispatchWrappedQuery(ctx, obj.QueryRaw, requestMsgID)
	case *mtprototl.InvokeWithoutUpdates:
		h.disableUpdates = true
		if h.server.updateHub != nil {
			h.server.updateHub.unbind(h.conn)
		}
		return h.dispatchWrappedQuery(ctx, obj.QueryRaw, requestMsgID)
	default:
		return h.invokeMethod(ctx, req)
	}
}

func (h *connHandler) invokeMethod(ctx context.Context, req TLObject) (TLObject, error) {
	constructorID := req.ConstructorID()
	methodHandler, ok := h.server.dispatcher.LookupMethod(constructorID)
	if !ok {
		if h.server.logger != nil {
			methodName := ""
			if named, ok := req.(interface{ Method() string }); ok {
				methodName = named.Method()
			}
			tlName := ""
			if named, ok := req.(interface{ TLName() string }); ok {
				tlName = named.TLName()
			}
			h.server.logger.Error("method not found", "constructor_id", fmt.Sprintf("0x%08x", constructorID), "method", methodName, "tl_name", tlName)
		}
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
	return normalizeResponse(resp)
}

func (h *connHandler) dispatchContainer(ctx context.Context, container *mtprototl.MsgContainer, requestMsgID int64) (TLObject, error) {
	_ = requestMsgID
	var responses []mtprototl.Message
	for _, msg := range container.Messages {
		if len(msg.BodyRaw) < 4 {
			continue
		}
		obj, _, err := decodeTLObject(h.server.dispatcher, msg.BodyRaw)
		if err != nil {
			continue
		}
		respObj, err := h.dispatchDecodedObject(ctx, obj, msg.MsgID)
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

func (h *connHandler) dispatchWrappedQuery(ctx context.Context, queryRaw []byte, requestMsgID int64) (TLObject, error) {
	if len(queryRaw) < 4 {
		return nil, io.ErrUnexpectedEOF
	}
	inner, _, err := decodeTLObject(h.server.dispatcher, queryRaw)
	if err != nil {
		return nil, err
	}
	return h.dispatchDecodedObject(ctx, inner, requestMsgID)
}

func (h *connHandler) dispatchInvokeWithLayer(ctx context.Context, req *mtprototl.InvokeWithLayer, requestMsgID int64) (TLObject, error) {
	layer, err := h.resolveLayer(int(req.Layer))
	if err != nil {
		return nil, err
	}
	ctx = withLayer(ctx, layer)
	if sess := SessionFromContext(ctx); sess != nil {
		sess.Layer = layer
		_ = h.server.sessions.Save(sess)
	}
	return h.dispatchWrappedQuery(ctx, req.QueryRaw, requestMsgID)
}

func (h *connHandler) dispatchInitConnection(ctx context.Context, req *mtprototl.InitConnection, requestMsgID int64) (TLObject, error) {
	if sess := SessionFromContext(ctx); sess != nil {
		sess.Data.Store("init.api_id", req.APIID)
		sess.Data.Store("init.device_model", req.DeviceModel)
		sess.Data.Store("init.system_version", req.SystemVersion)
		sess.Data.Store("init.app_version", req.AppVersion)
		sess.Data.Store("init.system_lang_code", req.SystemLangCode)
		sess.Data.Store("init.lang_pack", req.LangPack)
		sess.Data.Store("init.lang_code", req.LangCode)
		_ = h.server.sessions.Save(sess)
	}
	return h.dispatchWrappedQuery(ctx, req.QueryRaw, requestMsgID)
}

func (h *connHandler) resolveLayer(requested int) (int, error) {
	maxSupported := h.server.maxLayer
	if len(h.server.layers) > 0 {
		layers := append([]int(nil), h.server.layers...)
		sort.Ints(layers)
		maxSupported = layers[len(layers)-1]
		for _, layer := range layers {
			if layer == requested {
				return layer, nil
			}
		}
		if requested == 0 {
			return maxSupported, nil
		}
		if requested > maxSupported {
			return maxSupported, nil
		}
		return 0, NewBadRequestError("LAYER_INVALID")
	}

	if maxSupported <= 0 {
		if requested <= 0 {
			return 0, nil
		}
		return requested, nil
	}
	if requested <= 0 {
		return maxSupported, nil
	}
	if requested > maxSupported {
		return maxSupported, nil
	}
	return requested, nil
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

func (h *connHandler) sendBadMsgNotification(authKey crypto.AuthKey, keyID crypto.KeyID, badMsgID int64, badMsgSeq int32, code int32) error {
	body, err := encodeTLObject(&mtprototl.BadMsgNotification{
		BadMsgID:  badMsgID,
		BadMsgSeq: badMsgSeq,
		ErrorCode: code,
	})
	if err != nil {
		return err
	}
	inner := &mtproto.InnerData{
		Salt:      0,
		SessionID: 0,
		MsgID:     nextMsgID(),
		SeqNo:     0,
		Data:      body,
	}
	encResp, err := inner.Encrypt(authKey, keyID)
	if err != nil {
		return err
	}
	return h.conn.WriteMessage(serializeEncrypted(encResp))
}

func (h *connHandler) sendBadServerSalt(authKey crypto.AuthKey, keyID crypto.KeyID, badMsgID int64, badMsgSeq int32, newSalt int64) error {
	body, err := encodeTLObject(&mtprototl.BadServerSalt{
		BadMsgID:  badMsgID,
		BadMsgSeq: badMsgSeq,
		ErrorCode: 48,
		NewSalt:   newSalt,
	})
	if err != nil {
		return err
	}
	inner := &mtproto.InnerData{
		Salt:      0,
		SessionID: 0,
		MsgID:     nextMsgID(),
		SeqNo:     0,
		Data:      body,
	}
	encResp, err := inner.Encrypt(authKey, keyID)
	if err != nil {
		return err
	}
	return h.conn.WriteMessage(serializeEncrypted(encResp))
}

// sendRPCError converts an error to MTProto RPC error format and sends it.
func (h *connHandler) sendRPCError(requestMsgID int64, err error) error {
	// Convert error to MTProto rpc_error
	rpcErr := FromError(err)
	mtErr := &mtprototl.RPCError{
		ErrorCode:    rpcErr.ErrorCode,
		ErrorMessage: rpcErr.ErrorMessage,
	}

	errData, encErr := encodeTLObject(mtErr)
	if encErr != nil {
		// If encoding fails, send a generic internal error
		fallback := &mtprototl.RPCError{
			ErrorCode:    int32(Internal),
			ErrorMessage: "failed to encode error response",
		}
		errData, _ = encodeTLObject(fallback)
	}
	rpcResult := &mtprototl.RPCResult{
		ReqMsgID:  requestMsgID,
		ResultRaw: errData,
	}
	respData, encErr := encodeTLObject(rpcResult)
	if encErr != nil {
		return encErr
	}

	// Send the error response
	innerResp := &mtproto.InnerData{
		Salt:      0,
		SessionID: 0,
		MsgID:     nextMsgID(),
		SeqNo:     0,
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
