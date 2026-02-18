package client

import (
	"github.com/r6m/tlrpc"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
)

// TraceFunc receives trace events for MTProto traffic.
type TraceFunc func(TraceEvent)

// TraceEvent captures a single trace entry.
type TraceEvent struct {
	Direction    string
	Method       string
	TLName       string
	Constructor  uint32
	WrapperStack []string
}

func (c *Client) traceOutbound(payload []byte, msgID int64, seqNo int32) {
	_ = msgID
	_ = seqNo
	if c.trace == nil {
		return
	}
	obj, err := decodeTL(c.constructors, payload)
	if err != nil {
		return
	}
	c.trace(TraceEvent{
		Direction:    "outbound",
		Method:       methodName(obj),
		TLName:       tlName(obj),
		Constructor:  obj.ConstructorID(),
		WrapperStack: wrapperStack(obj, c.constructors),
	})
}

func (c *Client) traceInbound(obj tlrpc.TLObject) {
	if c.trace == nil || obj == nil {
		return
	}
	c.trace(TraceEvent{
		Direction:    "inbound",
		Method:       methodName(obj),
		TLName:       tlName(obj),
		Constructor:  obj.ConstructorID(),
		WrapperStack: wrapperStack(obj, c.constructors),
	})
}

func methodName(obj tlrpc.TLObject) string {
	if named, ok := obj.(interface{ Method() string }); ok {
		return named.Method()
	}
	return ""
}

func tlName(obj tlrpc.TLObject) string {
	if named, ok := obj.(interface{ TLName() string }); ok {
		return named.TLName()
	}
	return ""
}

func wrapperStack(obj tlrpc.TLObject, constructors map[uint32]func() tlrpc.TLObject) []string {
	var stack []string
	current := obj
	for {
		switch val := current.(type) {
		case *mtprototl.InvokeWithLayer:
			stack = append(stack, "invokeWithLayer")
			next, err := decodeTL(constructors, val.QueryRaw)
			if err != nil {
				return stack
			}
			current = next
		case *mtprototl.InitConnection:
			stack = append(stack, "initConnection")
			next, err := decodeTL(constructors, val.QueryRaw)
			if err != nil {
				return stack
			}
			current = next
		case *mtprototl.InvokeWithoutUpdates:
			stack = append(stack, "invokeWithoutUpdates")
			next, err := decodeTL(constructors, val.QueryRaw)
			if err != nil {
				return stack
			}
			current = next
		default:
			return stack
		}
	}
}
