package tlrpc

import (
	"context"
	"errors"
	"time"

	"github.com/r6m/tlrpc/crypto"
)

type temporaryKeyContextKey struct{}
type temporaryKeyRequest struct {
	keys      crypto.AuthKeyManager
	messageID int64
}

// BindTemporaryAuthKey is a cryptographic capability for a generated service
// implementing auth.bindTempAuthKey. It never creates application authorization.
func BindTemporaryAuthKey(ctx context.Context, permanentID, nonce int64, expiresAt int32, encrypted []byte) error {
	request, ok := ctx.Value(temporaryKeyContextKey{}).(temporaryKeyRequest)
	binding, bound := BindingFromContext(ctx)
	manager, supported := request.keys.(crypto.TemporaryAuthKeyManager)
	if !ok || !bound || !supported {
		return NewBadRequestError("TEMP_AUTH_KEY_EMPTY")
	}
	info, temporary, err := manager.TemporaryInfo(crypto.KeyID(binding.AuthKeyID))
	if err != nil || !temporary {
		return NewBadRequestError("TEMP_AUTH_KEY_EMPTY")
	}
	if _, temporary, err := manager.TemporaryInfo(crypto.KeyID(permanentID)); err != nil || temporary {
		return NewBadRequestError("ENCRYPTED_MESSAGE_INVALID")
	}
	permanent, err := manager.Get(crypto.KeyID(permanentID))
	if err != nil {
		return NewBadRequestError("ENCRYPTED_MESSAGE_INVALID")
	}
	if err := crypto.ValidateTemporaryKeyBinding(permanent, crypto.KeyID(binding.AuthKeyID), binding.SessionID, request.messageID, nonce, expiresAt, encrypted); err != nil {
		return NewBadRequestError("ENCRYPTED_MESSAGE_INVALID")
	}
	// Client/server clocks and second rounding may differ. A requested expiry
	// can shorten, but can never extend, the lifetime fixed at key generation.
	expires := time.Unix(int64(expiresAt), 0)
	if expires.After(info.ExpiresAt) {
		expires = info.ExpiresAt
	}
	now := time.Now()
	if !expires.After(now) || expires.After(now.Add(crypto.MaxTemporaryAuthKeyLifetime)) {
		return NewBadRequestError("EXPIRES_AT_INVALID")
	}
	if err := manager.BindTemporary(crypto.KeyID(binding.AuthKeyID), crypto.KeyID(permanentID), expires); err != nil {
		switch {
		case errors.Is(err, crypto.ErrTemporaryKeyAlreadyBound):
			return NewBadRequestError("TEMP_AUTH_KEY_ALREADY_BOUND")
		case errors.Is(err, crypto.ErrTemporaryKeyExpiry):
			return NewBadRequestError("EXPIRES_AT_INVALID")
		case errors.Is(err, crypto.ErrAuthKeyNotFound):
			return NewBadRequestError("TEMP_AUTH_KEY_EMPTY")
		default:
			return NewInternalError("INTERNAL_SERVER_ERROR")
		}
	}
	return nil
}
