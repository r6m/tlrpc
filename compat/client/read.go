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
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetDeadline(deadline)
		defer c.conn.SetDeadline(time.Time{})
	} else {
		_ = c.conn.SetDeadline(time.Time{})
	}

	for {
		packet, err := c.conn.ReadMessage()
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
