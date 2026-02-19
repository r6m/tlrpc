package tlrpc

import (
	"sync"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
)

type updateBinding struct {
	conn      connIO
	keyID     crypto.KeyID
	sessionID int64
	salt      int64
	msgIDs    *mtproto.MsgIDGenerator
	seqNos    *mtproto.SeqNoGenerator
}

type updateHub struct {
	mu     sync.RWMutex
	byUser map[int64][]updateBinding
}

func newUpdateHub() *updateHub {
	return &updateHub{byUser: make(map[int64][]updateBinding)}
}

func (h *updateHub) bind(userID int64, binding updateBinding) {
	if userID == 0 || binding.conn == nil {
		return
	}
	h.mu.Lock()
	h.byUser[userID] = append(h.byUser[userID], binding)
	h.mu.Unlock()
}

func (h *updateHub) unbind(conn connIO) {
	if conn == nil {
		return
	}
	h.mu.Lock()
	for userID, bindings := range h.byUser {
		filtered := bindings[:0]
		for _, binding := range bindings {
			if binding.conn != conn {
				filtered = append(filtered, binding)
			}
		}
		if len(filtered) == 0 {
			delete(h.byUser, userID)
			continue
		}
		h.byUser[userID] = filtered
	}
	h.mu.Unlock()
}

func (h *updateHub) publish(userID int64, update TLObject, authKeys crypto.AuthKeyManager) error {
	updateData, err := encodeTLObject(update)
	if err != nil {
		return err
	}

	h.mu.RLock()
	bindings := append([]updateBinding(nil), h.byUser[userID]...)
	h.mu.RUnlock()
	for _, binding := range bindings {
		authKey, err := authKeys.Get(binding.keyID)
		if err != nil {
			continue
		}
		if binding.msgIDs == nil || binding.seqNos == nil {
			continue
		}
		inner := &mtproto.InnerData{
			Salt:      binding.salt,
			SessionID: binding.sessionID,
			MsgID:     serverMsgID(binding.msgIDs.Next(), serverMsgIDPush),
			SeqNo:     binding.seqNos.Next(true),
			Data:      updateData,
		}
		enc, err := inner.Encrypt(authKey, binding.keyID)
		if err != nil {
			continue
		}
		if err := binding.conn.WriteMessage(serializeEncrypted(enc)); err != nil {
			continue
		}
	}
	return nil
}

// Publish sends a server-initiated update object to online connections for userID.
func (s *Server) Publish(userID int64, update TLObject) error {
	if s.updateHub == nil {
		return nil
	}
	return s.updateHub.publish(userID, update, s.authKeys)
}
