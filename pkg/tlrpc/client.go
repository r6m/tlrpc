// Package tlrpc provides client functionality for testing TLRPC servers.
package tlrpc

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/yourorg/tlrpc/pkg/transport"
)

// Client is a TLRPC client for testing.
type Client struct {
	conn     transport.Conn
	encoder  *encoder
	decoder  *decoder
	nextSeq  int32
}

// Dial creates a new client connection to the given address.
func Dial(addr string) (*Client, error) {
	return DialWithTransport(addr, transport.NewTCP())
}

// DialWithTransport creates a new client connection using the given transport.
func DialWithTransport(addr string, t transport.Transport) (*Client, error) {
	conn, err := t.Dial(addr)
	if err != nil {
		return nil, err
	}

	return NewClient(conn), nil
}

// NewClient creates a client from an existing connection.
func NewClient(conn transport.Conn) *Client {
	return &Client{
		conn:    conn,
		encoder: newEncoder(conn),
		decoder: newDecoder(conn),
	}
}

// Call makes an RPC call and returns the response.
func (c *Client) Call(ctx context.Context, req interface{}) (interface{}, error) {
	// Encode request
	seq := c.nextSeq
	c.nextSeq++

	if err := c.encoder.encode(seq, req); err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	// Set deadline if context has one
	if deadline, ok := ctx.Deadline(); ok {
		c.conn.SetDeadline(deadline)
	} else {
		c.conn.SetDeadline(time.Now().Add(30 * time.Second))
	}

	// Decode response
	resp, err := c.decoder.decode()
	if err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return resp, nil
}

// Close closes the client connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// encoder handles encoding messages
type encoder struct {
	conn transport.Conn
}

func newEncoder(conn transport.Conn) *encoder {
	return &encoder{conn: conn}
}

func (e *encoder) encode(seq int32, msg interface{}) error {
	// TODO: Implement proper MTProto encoding
	data := []byte(fmt.Sprintf("seq:%d msg:%v", seq, msg))
	return e.conn.WriteMessage(data)
}

// decoder handles decoding messages
type decoder struct {
	conn transport.Conn
}

func newDecoder(conn transport.Conn) *decoder {
	return &decoder{conn: conn}
}

func (d *decoder) decode() (interface{}, error) {
	// TODO: Implement proper MTProto decoding
	data, err := d.conn.ReadMessage()
	if err != nil {
		return nil, err
	}

	// For now, just return the data as-is
	return string(data), nil
}