package mtproto

import (
	"sync"
	"time"
)

// msgIDKind selects server msg_id low-bit behavior.
type msgIDKind int

const (
	msgIDResponse msgIDKind = iota
	msgIDPush
)

// MsgIDGenerator generates monotonic MTProto msg_id values.
type MsgIDGenerator struct {
	mu   sync.Mutex
	last int64
	now  func() time.Time
}

// NewMsgIDGenerator creates a new monotonic msg_id generator.
func NewMsgIDGenerator() *MsgIDGenerator {
	return &MsgIDGenerator{now: time.Now}
}

// Next returns the next monotonic, 4-aligned msg_id for generic use.
func (g *MsgIDGenerator) Next() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now()
	intPart := now.Unix()
	fracPart := int64(now.Nanosecond())
	fracPart &^= 3
	msgID := (intPart << 32) | fracPart

	if msgID <= g.last {
		msgID = g.last + 4
	}
	g.last = msgID
	return msgID
}

// nextServerMsgID returns a server msg_id with low bits set for response/push.
func (g *MsgIDGenerator) nextServerMsgID(kind msgIDKind) int64 {
	base := g.Next() &^ 3
	switch kind {
	case msgIDResponse:
		return base | 1
	case msgIDPush:
		return base | 3
	default:
		return base | 1
	}
}
