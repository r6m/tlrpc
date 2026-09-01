package transport

import (
	"bufio"
	"context"
	"io"
	"net"
	"reflect"
	"sync"
	"time"

	mtprotocodec "github.com/r6m/tlrpc/transport/mtproto_codec"
)

type wsMTProtoConn struct {
	base       *wsConn
	stream     *wsStream
	br         *bufio.Reader
	bw         *bufio.Writer
	negotiator *Negotiator
	codec      mtprotocodec.Codec
	r          io.Reader
	w          io.Writer
	negMu      sync.Mutex
	readMu     sync.Mutex
	writeMu    sync.Mutex
	isClient   bool
}

func newWSMTProtoConn(base *wsConn, config NegotiatorConfig, isClient bool) *wsMTProtoConn {
	stream := newWSStream(base.conn)
	return &wsMTProtoConn{
		base:       base,
		stream:     stream,
		br:         bufio.NewReader(stream),
		bw:         bufio.NewWriter(stream),
		negotiator: NewNegotiator(config),
		isClient:   isClient,
	}
}

func (c *wsMTProtoConn) ensureNegotiated() error {
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
		if err := c.stream.Flush(); err != nil {
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

func (c *wsMTProtoConn) ReadMessage(maxPayloadBytes int) ([]byte, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	if err := c.ensureNegotiated(); err != nil {
		return nil, err
	}
	for {
		payload, quickAck, err := c.codec.ReadPacket(c.r, maxPayloadBytes)
		if err != nil {
			if err == mtprotocodec.ErrQuickAck && quickAck != nil {
				continue
			}
			return nil, err
		}
		return payload, nil
	}
}

func (c *wsMTProtoConn) WriteMessage(payload []byte) error {
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
	if err := c.stream.Flush(); err != nil {
		return err
	}
	if flusher, ok := c.w.(interface{ Flush() error }); ok {
		return flusher.Flush()
	}
	return nil
}

func (c *wsMTProtoConn) Close() error                  { return c.base.Close() }
func (c *wsMTProtoConn) LocalAddr() net.Addr           { return c.base.LocalAddr() }
func (c *wsMTProtoConn) RemoteAddr() net.Addr          { return c.base.RemoteAddr() }
func (c *wsMTProtoConn) SetDeadline(t time.Time) error { return c.base.SetDeadline(t) }
func (c *wsMTProtoConn) SetReadDeadline(t time.Time) error {
	return c.base.SetReadDeadline(t)
}
func (c *wsMTProtoConn) SetWriteDeadline(t time.Time) error {
	return c.base.SetWriteDeadline(t)
}
func (c *wsMTProtoConn) Context() context.Context { return c.base.Context() }

func (c *wsMTProtoConn) TransportMode() string {
	if c.codec == nil {
		return ""
	}
	t := reflect.TypeOf(c.codec)
	if t == nil {
		return ""
	}
	if t.Kind() == reflect.Ptr {
		return t.Elem().Name()
	}
	return t.Name()
}
