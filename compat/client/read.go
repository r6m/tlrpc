package client

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/r6m/tlrpc"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
)

// ReadOne reads a single non-ack message from the server (useful for pushed updates).
func (c *Client) ReadOne(ctx context.Context) (tlrpc.TLObject, error) {
	if c.conn == nil {
		return nil, errors.New("compat client: missing connection")
	}
	if object := c.popPush(); object != nil {
		return object, nil
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
		defer func() { _ = c.conn.SetReadDeadline(time.Time{}) }()
	} else {
		if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
			return nil, err
		}
	}

	for {
		packet, err := c.conn.ReadMessage(0)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() && ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		inner, err := decryptPacket(packet, c.authKey)
		if err != nil {
			continue
		}
		obj, err := decodeTL(c.constructors, inner.Data)
		if err != nil {
			return nil, err
		}
		switch val := obj.(type) {
		case *mtprototl.MsgsAck:
			continue
		case *mtprototl.NewSessionCreated:
			continue
		case *mtprototl.MsgContainer:
			for _, msg := range val.Messages {
				if len(msg.BodyRaw) < 4 {
					continue
				}
				item, err := decodeTL(c.constructors, msg.BodyRaw)
				if err != nil {
					continue
				}
				if _, ok := item.(*mtprototl.MsgsAck); ok {
					continue
				}
				if _, ok := item.(*mtprototl.NewSessionCreated); ok {
					continue
				}
				if _, ok := item.(*mtprototl.RPCResult); ok {
					continue
				}
				c.traceInbound(item)
				return item, nil
			}
			continue
		case *mtprototl.RPCResult:
			// Skip RPC results when waiting for push updates.
			continue
		default:
			c.traceInbound(obj)
			return obj, nil
		}
	}
}
