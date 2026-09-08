package runtime

import (
	"encoding/binary"

	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
)

// Handshake acknowledgements are plaintext controls, not key-exchange steps.
// Android sends them before each subsequent DH request and after dh_gen_ok.
// They never acknowledge encrypted-session output or produce a reply.
func (c *Connection) handleHandshakeAcknowledgement(body []byte) (bool, error) {
	if len(body) < 4 || binary.LittleEndian.Uint32(body) != mtprototl.MsgsAckID {
		return false, nil
	}
	if c.handshakeSession == nil {
		return true, ErrConnectionProtocol
	}
	// The shared control decoder bounds the vector before reading IDs and
	// rejects truncated fields and trailing bytes. No reliability state changes.
	return true, decodeControl(body, &mtprototl.MsgsAck{})
}
