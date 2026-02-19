package mtproto

import (
	"sync"
	"time"
)

// MsgIDGenerator generates monotonic MTProto msg_id values.
type MsgIDGenerator struct {
	mu   sync.Mutex
	last int64
}

// NewMsgIDGenerator creates a new monotonic msg_id generator.
func NewMsgIDGenerator() *MsgIDGenerator {
	return &MsgIDGenerator{}
}

// Next returns the next monotonic, 4-aligned msg_id.
func (g *MsgIDGenerator) Next() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	msgID := (now.UnixNano() / int64(time.Millisecond)) << 32
	msgID |= (now.UnixNano() % int64(time.Millisecond)) << 2
	msgID &^= 3

	if msgID <= g.last {
		msgID = g.last + 4
	}
	g.last = msgID
	return msgID
}
