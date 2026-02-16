package tlrpc

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
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

type netConn struct {
	conn   net.Conn
	ctx    context.Context
	cancel context.CancelFunc
}

func newNetConn(conn net.Conn) *netConn {
	ctx, cancel := context.WithCancel(context.Background())
	return &netConn{conn: conn, ctx: ctx, cancel: cancel}
}

func (c *netConn) ReadMessage() ([]byte, error) {
	return readFrame(c.conn)
}

func (c *netConn) WriteMessage(payload []byte) error {
	return writeFrame(c.conn, payload)
}

func (c *netConn) Close() error {
	c.cancel()
	return c.conn.Close()
}

func (c *netConn) Context() context.Context {
	return c.ctx
}

func readFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	length := binary.LittleEndian.Uint32(header[:])
	if length < 4 {
		return nil, errors.New("tlrpc: invalid frame length")
	}
	payloadLen := int(length - 4)
	if payloadLen < 0 {
		return nil, errors.New("tlrpc: invalid frame length")
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	padding := (4 - (payloadLen % 4)) % 4
	if padding > 0 {
		var pad [3]byte
		if _, err := io.ReadFull(r, pad[:padding]); err != nil {
			return nil, err
		}
	}
	return payload, nil
}

func writeFrame(w io.Writer, payload []byte) error {
	length := uint32(len(payload) + 4)
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], length)
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	padding := (4 - (len(payload) % 4)) % 4
	if padding > 0 {
		_, err := w.Write(make([]byte, padding))
		return err
	}
	return nil
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

	// Check if this is a container message
	if len(inner.Data) >= 4 {
		constructorID := binary.LittleEndian.Uint32(inner.Data[:4])
		if constructorID == 0x73f1f8dc { // msg_container
			return h.handleContainerMessage(ctx, sess, authKey, keyID, inner)
		}
		// Check for resend/state requests
		if constructorID == 0x04deb57d || constructorID == 0x7da458c8 { // msgs_state_req or msg_resend_req
			// For now, just acknowledge these requests without full resend logic
			return h.sendAcknowledgment(authKey, keyID, inner.MsgID)
		}
	}

	// Read constructor ID
	if len(inner.Data) < 4 {
		return io.ErrUnexpectedEOF
	}
	constructorID := binary.LittleEndian.Uint32(inner.Data[:4])

	// Look up method handler directly by constructor ID
	methodHandler, ok := h.server.dispatcher.LookupMethod(constructorID)
	if !ok {
		return NewNotFoundError("METHOD_NOT_FOUND")
	}

	// Decode the request only after confirming we have a handler
	constructor, ok := h.server.dispatcher.LookupConstructor(constructorID)
	if !ok {
		return NewNotFoundError("UNKNOWN_CONSTRUCTOR")
	}

	req := constructor()
	if deser, ok := req.(interface{ DeserializeTL(io.Reader) error }); ok {
		r := bytes.NewReader(inner.Data)
		// Skip constructor ID (already read)
		if _, err := mtproto.ReadUint32(r); err != nil {
			return err
		}
		if err := deser.DeserializeTL(r); err != nil {
			return err
		}
	}

	var resp interface{}

	// Apply new gRPC-like unary interceptors
	if len(h.server.unaryInterceptors) > 0 {
		info := &UnaryServerInfo{FullMethod: fmt.Sprintf("constructor_%08x", constructorID)}
		chainedInterceptor := ChainUnaryInterceptors(h.server.unaryInterceptors...)
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			return methodHandler(ctx, req.(TLObject))
		}
		resp, err = chainedInterceptor(ctx, req, info, handler)
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

	// Send acknowledgment for the received message
	return h.sendAcknowledgment(authKey, keyID, inner.MsgID)
}

// handleContainerMessage processes a container of batched messages
func (h *connHandler) handleContainerMessage(ctx context.Context, sess *session.Session, authKey crypto.AuthKey, keyID crypto.KeyID, containerInner *mtproto.InnerData) error {
	r := bytes.NewReader(containerInner.Data[4:]) // Skip constructor

	// Read vector header
	vectorLen, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}

	type containerResponse struct {
		msgID int64
		seqNo int32
		data  []byte
	}

	var responses []containerResponse
	var ackMsgIDs []int64

	// Process each message in the container
	for i := uint32(0); i < vectorLen; i++ {
		// Read message fields
		msgID, err := mtproto.ReadInt64(r)
		if err != nil {
			return err
		}
		seqNo, err := mtproto.ReadInt32(r)
		if err != nil {
			return err
		}
		msgData, err := mtproto.ReadBytes(r)
		if err != nil {
			return err
		}

		// Collect message ID for acknowledgment
		ackMsgIDs = append(ackMsgIDs, msgID)

		// Process the individual message
		if len(msgData) < 4 {
			continue // Skip invalid messages
		}
		constructorID := binary.LittleEndian.Uint32(msgData[:4])

		// Look up method handler directly by constructor ID
		methodHandler, ok := h.server.dispatcher.LookupMethod(constructorID)
		if !ok {
			continue // Skip unknown methods
		}

		// Decode the request
		constructor, ok := h.server.dispatcher.LookupConstructor(constructorID)
		if !ok {
			continue // Skip unknown constructors
		}

		req := constructor()
		if deser, ok := req.(interface{ DeserializeTL(io.Reader) error }); ok {
			r := bytes.NewReader(msgData)
			// Skip constructor ID (already read)
			if _, err := mtproto.ReadUint32(r); err != nil {
				continue
			}
			if err := deser.DeserializeTL(r); err != nil {
				continue
			}
		}

		var resp interface{}

		// Apply interceptors
		if len(h.server.unaryInterceptors) > 0 {
			info := &UnaryServerInfo{FullMethod: fmt.Sprintf("constructor_%08x", constructorID)}
			chainedInterceptor := ChainUnaryInterceptors(h.server.unaryInterceptors...)
			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				return methodHandler(ctx, req.(TLObject))
			}
			resp, err = chainedInterceptor(ctx, req, info, handler)
		}

		if err != nil || resp == nil {
			continue // Skip failed messages or messages without responses
		}

		respObj, ok := resp.(TLObject)
		if !ok {
			continue // Skip invalid responses
		}

		respData, err := encodeTLObject(respObj)
		if err != nil {
			continue // Skip encoding errors
		}

		responses = append(responses, containerResponse{
			msgID: msgID,
			seqNo: seqNo,
			data:  respData,
		})
	}

	// Send responses
	if len(responses) == 1 {
		// Single response - send as regular message
		innerResp := &mtproto.InnerData{
			Salt:      containerInner.Salt,
			SessionID: containerInner.SessionID,
			MsgID:     nextMsgID(),
			SeqNo:     responses[0].seqNo,
			Data:      responses[0].data,
		}
		encResp, err := innerResp.Encrypt(authKey, keyID)
		if err != nil {
			return err
		}
		return h.conn.WriteMessage(serializeEncrypted(encResp))
	} else if len(responses) > 1 {
		// Multiple responses - send as container
		containerBuf := &bytes.Buffer{}

		// Write container constructor
		if err := mtproto.WriteUint32(containerBuf, 0x73f1f8dc); err != nil {
			return err
		}

		// Write vector header
		if err := mtproto.WriteVectorHeader(containerBuf, len(responses)); err != nil {
			return err
		}

		// Write each response
		for _, resp := range responses {
			if err := mtproto.WriteInt64(containerBuf, nextMsgID()); err != nil {
				return err
			}
			if err := mtproto.WriteInt32(containerBuf, resp.seqNo); err != nil {
				return err
			}
			if err := mtproto.WriteBytes(containerBuf, resp.data); err != nil {
				return err
			}
		}

		innerResp := &mtproto.InnerData{
			Salt:      containerInner.Salt,
			SessionID: containerInner.SessionID,
			MsgID:     nextMsgID(),
			SeqNo:     nextSeqNo(sess),
			Data:      containerBuf.Bytes(),
		}
		encResp, err := innerResp.Encrypt(authKey, keyID)
		if err != nil {
			return err
		}
		return h.conn.WriteMessage(serializeEncrypted(encResp))
	}

	// Send acknowledgments for all processed messages
	return h.sendAcknowledgment(authKey, keyID, ackMsgIDs...)
}

// sendAcknowledgment sends an acknowledgment for received message IDs
func (h *connHandler) sendAcknowledgment(authKey crypto.AuthKey, keyID crypto.KeyID, msgIDs ...int64) error {
	if len(msgIDs) == 0 {
		return nil
	}

	ackBuf := &bytes.Buffer{}
	if err := mtproto.WriteUint32(ackBuf, 0x62d6b459); err != nil { // msgs_ack
		return err
	}

	if err := mtproto.WriteVectorHeader(ackBuf, len(msgIDs)); err != nil {
		return err
	}

	for _, msgID := range msgIDs {
		if err := mtproto.WriteInt64(ackBuf, msgID); err != nil {
			return err
		}
	}

	innerAck := &mtproto.InnerData{
		Salt:      0, // Use 0 for acks
		SessionID: 0, // Use 0 for acks
		MsgID:     nextMsgID(),
		SeqNo:     0, // Acks don't need sequence numbers
		Data:      ackBuf.Bytes(),
	}

	encAck, err := innerAck.Encrypt(authKey, keyID)
	if err != nil {
		return err
	}

	return h.conn.WriteMessage(serializeEncrypted(encAck))
}

// sendRPCError converts an error to MTProto RPC error format and sends it.
func (h *connHandler) sendRPCError(requestMsgID int64, err error) error {
	// Convert error to RPCError
	rpcErr := FromError(err)

	// Encode the RPC error as TL object
	respData, encErr := encodeTLObject(rpcErr)
	if encErr != nil {
		// If encoding fails, send a generic internal error
		genericErr := NewInternalError("failed to encode error response")
		respData, _ = encodeTLObject(genericErr)
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
