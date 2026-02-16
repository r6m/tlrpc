package tlrpc

import (
	"bytes"
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
