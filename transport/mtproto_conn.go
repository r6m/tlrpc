package transport

import (
	"bufio"
	"context"
	"io"
	"net"
	"sync"
	"time"

	mtprotocodec "github.com/r6m/tlrpc/transport/mtproto_codec"
)

type MTProtoConn struct {
	conn       net.Conn
	br         *bufio.Reader
	bw         *bufio.Writer
	negotiator *Negotiator
	codec      mtprotocodec.Codec
	r          io.Reader
	w          io.Writer
	ctx        context.Context
	cancel     context.CancelFunc
	negMu      sync.Mutex
	readMu     sync.Mutex
	writeMu    sync.Mutex
	isClient   bool
}

func NewMTProtoConn(conn net.Conn, config NegotiatorConfig) *MTProtoConn {
	ctx, cancel := context.WithCancel(context.Background())
	return &MTProtoConn{
		conn:       conn,
		br:         bufio.NewReader(conn),
		bw:         bufio.NewWriter(conn),
		negotiator: NewNegotiator(config),
		r:          nil,
		w:          nil,
		ctx:        ctx,
		cancel:     cancel,
	}
}

func NewClientMTProtoConn(conn net.Conn, config NegotiatorConfig) *MTProtoConn {
	c := NewMTProtoConn(conn, config)
	c.isClient = true
	return c
}

func (c *MTProtoConn) ensureNegotiated() error {
	c.negMu.Lock()
	defer c.negMu.Unlock()
	if c.codec != nil {
		return nil
	}
	var rw io.ReadWriter
	var codec mtprotocodec.Codec
	var err error
	if c.isClient {
		rw, codec, err = c.negotiator.ClientNegotiate(&bufferedReadWriter{r: c.br, w: c.bw})
		if err != nil {
			return err
		}
		if err := c.bw.Flush(); err != nil {
			return err
		}
	} else {
		rw, codec, err = c.negotiator.Negotiate(c.br, c.bw)
		if err != nil {
			return err
		}
	}
	c.r = rw
	c.w = rw
	c.codec = codec
	return nil
}

// ReadMessage reads a single MTProto transport packet.
func (c *MTProtoConn) ReadMessage() ([]byte, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	if err := c.ensureNegotiated(); err != nil {
		return nil, err
	}
	for {
		payload, quickAck, err := c.codec.ReadPacket(c.r)
		if err != nil {
			if err == mtprotocodec.ErrQuickAck && quickAck != nil {
				continue
			}
			return nil, err
		}
		return payload, nil
	}
}

// WriteMessage writes a single MTProto transport packet.
func (c *MTProtoConn) WriteMessage(payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := c.ensureNegotiated(); err != nil {
		return err
	}
	if err := c.codec.WritePacket(c.w, payload); err != nil {
		return err
	}
	if c.bw != nil {
		if err := c.bw.Flush(); err != nil {
			return err
		}
	}
	if flusher, ok := c.w.(interface{ Flush() error }); ok {
		return flusher.Flush()
	}
	return nil
}

func (c *MTProtoConn) Close() error {
	c.cancel()
	return c.conn.Close()
}

func (c *MTProtoConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *MTProtoConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *MTProtoConn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

func (c *MTProtoConn) Context() context.Context {
	return c.ctx
}

type bufferedReadWriter struct {
	r *bufio.Reader
	w *bufio.Writer
}

func (b *bufferedReadWriter) Read(p []byte) (int, error) {
	return b.r.Read(p)
}

func (b *bufferedReadWriter) Write(p []byte) (int, error) {
	return b.w.Write(p)
}
