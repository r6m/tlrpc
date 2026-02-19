package tlrpc

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/transport"
)

type serverConn struct {
	base  transport.Conn
	srv   *Server
	state *connHandlerState
	mu    sync.Mutex
}

func newServerConn(srv *Server, base transport.Conn, state *connHandlerState) *serverConn {
	return &serverConn{base: base, srv: srv, state: state}
}

func (c *serverConn) ReadMessage() ([]byte, error) { return c.base.ReadMessage() }
func (c *serverConn) WriteMessage(payload []byte) error {
	return c.base.WriteMessage(payload)
}
func (c *serverConn) Close() error                  { return c.base.Close() }
func (c *serverConn) LocalAddr() net.Addr           { return c.base.LocalAddr() }
func (c *serverConn) RemoteAddr() net.Addr          { return c.base.RemoteAddr() }
func (c *serverConn) SetDeadline(t time.Time) error { return c.base.SetDeadline(t) }
func (c *serverConn) Context() context.Context      { return c.base.Context() }

func (c *serverConn) Send(obj TLObject) error {
	if obj == nil {
		return errors.New("tlrpc: nil object")
	}
	if c.srv == nil || c.state == nil {
		return errors.New("tlrpc: missing server context")
	}
	if c.state.session == nil {
		return errors.New("tlrpc: missing session")
	}
	if c.state.authKeyID == 0 {
		return ErrUnauthorized
	}

	payload, err := encodeTLObject(obj)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state.msgIDs == nil {
		c.state.msgIDs = mtproto.NewMsgIDGenerator()
	}
	if c.state.seqNos == nil {
		c.state.seqNos = mtproto.NewSeqNoGenerator(0)
	}

	msgID := serverMsgID(c.state.msgIDs.Next(), serverMsgIDPush)
	seqNo := c.state.seqNos.Next(true)
	inner := &mtproto.InnerData{
		Salt:      c.state.session.ServerSalt,
		SessionID: c.state.session.SessionID,
		MsgID:     msgID,
		SeqNo:     seqNo,
		Data:      payload,
	}

	authKey, err := c.srv.authKeys.Get(c.state.authKeyID)
	if err != nil {
		return err
	}
	enc, err := inner.Encrypt(authKey, c.state.authKeyID)
	if err != nil {
		return err
	}
	return c.base.WriteMessage(serializeEncrypted(enc))
}
