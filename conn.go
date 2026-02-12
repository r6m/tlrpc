package tlrpc

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/session"
)

type connHandler struct {
	server *Server
	conn   connIO
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
		if err := h.handleMessage(payload); err != nil {
			return err
		}
	}
}

func (h *connHandler) handleMessage(payload []byte) error {
	if len(payload) < 8 {
		return io.ErrUnexpectedEOF
	}
	keyID := crypto.KeyID(binary.LittleEndian.Uint64(payload[:8]))
	if keyID == 0 {
		return ErrUnauthorized
	}
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

	sess, err := h.server.sessions.Get(keyID)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			sess, err = h.server.sessions.Create(keyID)
		}
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
		return ErrMethodNotFound
	}
	method, ok := h.server.registry.GetMethod(methodName)
	if !ok {
		return ErrMethodNotFound
	}

	handler := method.Handler
	if len(h.server.interceptors) > 0 {
		handler = ChainInterceptors(h.server.interceptors...)(handler)
	}
	resp, err := handler(ctx, req)
	if err != nil {
		return err
	}
	if resp == nil {
		return nil
	}
	respObj, ok := resp.(TLObject)
	if !ok {
		return errors.New("tlrpc: response does not implement TLObject")
	}
	respData, err := h.server.codec.Encode(layerFromSession(sess), respObj)
	if err != nil {
		return err
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

func layerFromSession(sess *session.Session) int {
	if sess == nil {
		return 0
	}
	return sess.Layer
}

func nextMsgID() int64 {
	return time.Now().UnixNano() &^ 3
}

func nextSeqNo(sess *session.Session) int32 {
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
