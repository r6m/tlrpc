package tlrpc

import (
	"sync"

	"github.com/r6m/tlrpc/crypto"
)

type connHandlerState struct {
	onceBound sync.Once
	binding   Binding
	authKeyID crypto.KeyID
}
