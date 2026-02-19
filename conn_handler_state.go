package tlrpc

import (
	"sync"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/session"
)

type connHandlerState struct {
	onceBound sync.Once
	binding   Binding
	authKeyID crypto.KeyID
	msgIDs    *mtproto.MsgIDGenerator
	seqNos    *mtproto.SeqNoGenerator
	session   *session.Session
}
