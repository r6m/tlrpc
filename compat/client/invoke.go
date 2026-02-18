package client

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/r6m/tlrpc"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
)

// BadServerSaltError indicates the server rejected a salt and supplied a new one.
type BadServerSaltError struct {
	MsgID   int64
	NewSalt int64
}

func (e *BadServerSaltError) Error() string {
	return fmt.Sprintf("bad_server_salt for msg_id=%d new_salt=%d", e.MsgID, e.NewSalt)
}

// BadMsgNotificationError indicates a bad_msg_notification response.
type BadMsgNotificationError struct {
	MsgID  int64
	Code   int32
	SeqNo  int32
	BadMsg int64
}

func (e *BadMsgNotificationError) Error() string {
	return fmt.Sprintf("bad_msg_notification bad_msg_id=%d error_code=%d", e.BadMsg, e.Code)
}

func (c *Client) readResponse(ctx context.Context, reqMsgID int64) (tlrpc.TLObject, error) {
	_ = ctx
	for i := 0; i < 10; i++ {
		packet, err := c.conn.ReadMessage()
		if err != nil {
			return nil, err
		}
		inner, err := decryptPacket(packet, c.authKey)
		if err != nil {
			return nil, err
		}
		obj, err := decodeTL(c.constructors, inner.Data)
		if err != nil {
			return nil, err
		}
		resp, handled, err := c.handleDecoded(obj, reqMsgID)
		if handled {
			return resp, err
		}
	}
	return nil, errors.New("compat client: response not received")
}

func (c *Client) handleDecoded(obj tlrpc.TLObject, reqMsgID int64) (tlrpc.TLObject, bool, error) {
	switch val := obj.(type) {
	case *mtprototl.MsgsAck:
		return nil, false, nil
	case *mtprototl.MsgContainer:
		for _, msg := range val.Messages {
			if len(msg.BodyRaw) < 4 {
				continue
			}
			inner, err := decodeTL(c.constructors, msg.BodyRaw)
			if err != nil {
				continue
			}
			resp, handled, err := c.handleDecoded(inner, reqMsgID)
			if handled {
				return resp, true, err
			}
		}
		return nil, false, nil
	case *mtprototl.RPCResult:
		if val.ReqMsgID != reqMsgID {
			return nil, false, nil
		}
		resp, err := c.decodeRPCResult(val)
		if err == nil {
			c.traceInbound(resp)
		}
		return resp, true, err
	case *mtprototl.BadServerSalt:
		return nil, true, &BadServerSaltError{MsgID: val.BadMsgID, NewSalt: val.NewSalt}
	case *mtprototl.BadMsgNotification:
		return nil, true, &BadMsgNotificationError{
			MsgID:  reqMsgID,
			Code:   val.ErrorCode,
			SeqNo:  val.BadMsgSeq,
			BadMsg: val.BadMsgID,
		}
	default:
		c.traceInbound(val)
		return val, true, nil
	}
}

func (c *Client) decodeRPCResult(result *mtprototl.RPCResult) (tlrpc.TLObject, error) {
	if len(result.ResultRaw) == 0 {
		return nil, nil
	}
	obj, err := decodeTL(c.constructors, result.ResultRaw)
	if err != nil {
		return nil, err
	}
	switch val := obj.(type) {
	case *mtprototl.GzipPacked:
		gr, err := gzip.NewReader(bytes.NewReader(val.PackedData))
		if err != nil {
			return nil, err
		}
		defer func() { _ = gr.Close() }()
		unpacked, err := io.ReadAll(gr)
		if err != nil {
			return nil, err
		}
		return decodeTL(c.constructors, unpacked)
	case *mtprototl.RPCError:
		return nil, tlrpc.NewRPCError(val.ErrorCode, val.ErrorMessage)
	default:
		return obj, nil
	}
}
