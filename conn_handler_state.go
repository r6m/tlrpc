package tlrpc

import (
	"sync"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
)

type connHandlerState struct {
	onceBound sync.Once
	binding   Binding
	authKeyID crypto.KeyID
	msgIDs    *mtproto.MsgIDGenerator
	seqNos    *mtproto.SeqNoGenerator
}
