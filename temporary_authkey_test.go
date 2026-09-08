package tlrpc

import (
	"context"
	"crypto/aes"
	"crypto/rand"
	"crypto/sha1"
	"testing"
	"time"

	"github.com/gotd/ige"
	gotdbin "github.com/gotd/td/bin"
	gotdcrypto "github.com/gotd/td/crypto"
	tlrpccrypto "github.com/r6m/tlrpc/crypto"
)

const (
	temporaryBindingNonce     int64 = 17
	temporaryBindingSessionID int64 = 31
	temporaryBindingMessageID int64 = 1800000000<<32 | 4
)

type temporaryBindingFixture struct {
	manager     tlrpccrypto.TemporaryAuthKeyManager
	permanent   gotdcrypto.Key
	permanentID tlrpccrypto.KeyID
	temporaryID tlrpccrypto.KeyID
	context     context.Context
}

type expiredStoredKeyManager struct {
	*tlrpccrypto.MemoryAuthKeyManager
	temporaryID tlrpccrypto.KeyID
	expiresAt   time.Time
	bindCalled  bool
}

func TestBindTemporaryAuthKeyAcceptsAuthenticatedClockSkew(t *testing.T) {
	now := time.Now()
	storedExpiry := now.Add(tlrpccrypto.MaxTemporaryAuthKeyLifetime - 5*time.Minute)
	requestedExpiry := int32(now.Add(tlrpccrypto.MaxTemporaryAuthKeyLifetime + 2*time.Minute).Unix())
	fixture := newTemporaryBindingFixture(t, storedExpiry)
	encrypted := fixture.encrypt(t, requestedExpiry)

	if err := fixture.bind(fixture.permanentID, requestedExpiry, encrypted); err != nil {
		t.Fatalf("BindTemporaryAuthKey() error = %v", err)
	}

	info, temporary, err := fixture.manager.TemporaryInfo(fixture.temporaryID)
	if err != nil || !temporary {
		t.Fatalf("TemporaryInfo() = %#v, %t, %v", info, temporary, err)
	}
	if !info.ExpiresAt.Equal(storedExpiry) {
		t.Fatalf("bound expiry = %v, want stored expiry %v", info.ExpiresAt, storedExpiry)
	}
}

func TestBindTemporaryAuthKeyShortensStoredExpiry(t *testing.T) {
	storedExpiry := time.Now().Add(2 * time.Hour)
	requestedExpiry := int32(time.Now().Add(30 * time.Minute).Unix())
	fixture := newTemporaryBindingFixture(t, storedExpiry)
	encrypted := fixture.encrypt(t, requestedExpiry)

	if err := fixture.bind(fixture.permanentID, requestedExpiry, encrypted); err != nil {
		t.Fatalf("BindTemporaryAuthKey() error = %v", err)
	}

	info, temporary, err := fixture.manager.TemporaryInfo(fixture.temporaryID)
	if err != nil || !temporary {
		t.Fatalf("TemporaryInfo() = %#v, %t, %v", info, temporary, err)
	}
	want := time.Unix(int64(requestedExpiry), 0)
	if !info.ExpiresAt.Equal(want) {
		t.Fatalf("bound expiry = %v, want requested expiry %v", info.ExpiresAt, want)
	}
}

func TestBindTemporaryAuthKeyRejectsForgedOuterExpiry(t *testing.T) {
	storedExpiry := time.Now().Add(2 * time.Hour)
	signedExpiry := int32(time.Now().Add(time.Hour).Unix())
	fixture := newTemporaryBindingFixture(t, storedExpiry)
	encrypted := fixture.encrypt(t, signedExpiry)

	err := fixture.bind(fixture.permanentID, signedExpiry+1, encrypted)
	requireTemporaryBindingError(t, err, "ENCRYPTED_MESSAGE_INVALID")
	requireTemporaryKeyUnbound(t, fixture, storedExpiry)
}

func TestBindTemporaryAuthKeyRejectsExpiredRequest(t *testing.T) {
	storedExpiry := time.Now().Add(2 * time.Hour)
	requestedExpiry := int32(time.Now().Add(-time.Minute).Unix())
	fixture := newTemporaryBindingFixture(t, storedExpiry)
	encrypted := fixture.encrypt(t, requestedExpiry)

	err := fixture.bind(fixture.permanentID, requestedExpiry, encrypted)
	requireTemporaryBindingError(t, err, "EXPIRES_AT_INVALID")
	requireTemporaryKeyUnbound(t, fixture, storedExpiry)
}

func TestBindTemporaryAuthKeyRejectsExpiredStoredKey(t *testing.T) {
	permanent := randomGotdKey(t)
	permanentID := tlrpccrypto.KeyID(permanent.WithID().IntID())
	temporaryID := tlrpccrypto.KeyID(randomGotdKey(t).WithID().IntID())
	keys := tlrpccrypto.NewMemoryAuthKeyManager()
	if err := keys.Put(permanentID, tlrpccrypto.AuthKey(permanent)); err != nil {
		t.Fatal(err)
	}
	manager := &expiredStoredKeyManager{
		MemoryAuthKeyManager: keys,
		temporaryID:          temporaryID,
		expiresAt:            time.Now().Add(-time.Minute),
	}
	fixture := temporaryBindingFixture{
		manager:     manager,
		permanent:   permanent,
		permanentID: permanentID,
		temporaryID: temporaryID,
		context:     temporaryBindingContext(manager, temporaryID),
	}
	requestedExpiry := int32(time.Now().Add(time.Hour).Unix())
	encrypted := fixture.encrypt(t, requestedExpiry)

	err := fixture.bind(permanentID, requestedExpiry, encrypted)
	requireTemporaryBindingError(t, err, "EXPIRES_AT_INVALID")
	if manager.bindCalled {
		t.Fatal("BindTemporary called for an expired stored key")
	}
}

func TestBindTemporaryAuthKeyRejectsRebindingDifferentPermanentKey(t *testing.T) {
	storedExpiry := time.Now().Add(2 * time.Hour)
	requestedExpiry := int32(time.Now().Add(time.Hour).Unix())
	fixture := newTemporaryBindingFixture(t, storedExpiry)
	encrypted := fixture.encrypt(t, requestedExpiry)
	if err := fixture.bind(fixture.permanentID, requestedExpiry, encrypted); err != nil {
		t.Fatalf("initial BindTemporaryAuthKey() error = %v", err)
	}

	otherPermanent := randomGotdKey(t)
	otherPermanentID := tlrpccrypto.KeyID(otherPermanent.WithID().IntID())
	if err := fixture.manager.Put(otherPermanentID, tlrpccrypto.AuthKey(otherPermanent)); err != nil {
		t.Fatal(err)
	}
	encrypted = fixture.encryptWithPermanent(t, otherPermanent, requestedExpiry)

	err := fixture.bind(otherPermanentID, requestedExpiry, encrypted)
	requireTemporaryBindingError(t, err, "TEMP_AUTH_KEY_ALREADY_BOUND")
	info, temporary, infoErr := fixture.manager.TemporaryInfo(fixture.temporaryID)
	if infoErr != nil || !temporary {
		t.Fatalf("TemporaryInfo() = %#v, %t, %v", info, temporary, infoErr)
	}
	if info.PermanentKeyID != fixture.permanentID {
		t.Fatalf("permanent key ID = %d, want original %d", info.PermanentKeyID, fixture.permanentID)
	}
}

func newTemporaryBindingFixture(t *testing.T, expiresAt time.Time) temporaryBindingFixture {
	t.Helper()
	manager := tlrpccrypto.NewMemoryAuthKeyManager()
	permanent := randomGotdKey(t)
	permanentID := tlrpccrypto.KeyID(permanent.WithID().IntID())
	if err := manager.Put(permanentID, tlrpccrypto.AuthKey(permanent)); err != nil {
		t.Fatal(err)
	}
	temporary := randomGotdKey(t)
	temporaryID := tlrpccrypto.KeyID(temporary.WithID().IntID())
	if err := manager.PutTemporary(temporaryID, tlrpccrypto.AuthKey(temporary), expiresAt); err != nil {
		t.Fatal(err)
	}
	return temporaryBindingFixture{
		manager:     manager,
		permanent:   permanent,
		permanentID: permanentID,
		temporaryID: temporaryID,
		context:     temporaryBindingContext(manager, temporaryID),
	}
}

func temporaryBindingContext(manager tlrpccrypto.TemporaryAuthKeyManager, temporaryID tlrpccrypto.KeyID) context.Context {
	ctx := context.WithValue(context.Background(), contextKeyBinding, Binding{
		ConnectionID: 1,
		AuthKeyID:    int64(temporaryID),
		SessionID:    temporaryBindingSessionID,
	})
	ctx = context.WithValue(ctx, temporaryKeyContextKey{}, temporaryKeyRequest{
		keys:      manager,
		messageID: temporaryBindingMessageID,
	})
	return ctx
}

func (f temporaryBindingFixture) bind(permanentID tlrpccrypto.KeyID, expiresAt int32, encrypted []byte) error {
	return BindTemporaryAuthKey(f.context, int64(permanentID), temporaryBindingNonce, expiresAt, encrypted)
}

func (f temporaryBindingFixture) encrypt(t *testing.T, expiresAt int32) []byte {
	t.Helper()
	return f.encryptWithPermanent(t, f.permanent, expiresAt)
}

// encryptWithPermanent is a test-only port of github.com/gotd/td v0.140.0
// crypto.EncryptBindMessage. This repository pins v0.139.0, which provides the
// independent wire, MTProto v1 KDF, and IGE primitives used by the port but
// predates the convenience helper used by tgserver's client test.
func (f temporaryBindingFixture) encryptWithPermanent(t *testing.T, permanent gotdcrypto.Key, expiresAt int32) []byte {
	t.Helper()

	payload := &gotdbin.Buffer{}
	payload.PutID(0x75a3f765)
	payload.PutLong(temporaryBindingNonce)
	payload.PutLong(int64(f.temporaryID))
	payload.PutLong(permanent.WithID().IntID())
	payload.PutLong(temporaryBindingSessionID)
	payload.PutInt32(expiresAt)

	plaintext := &gotdbin.Buffer{}
	randomPrefix := make([]byte, 16)
	if _, err := rand.Read(randomPrefix); err != nil {
		t.Fatal(err)
	}
	plaintext.Put(randomPrefix)
	plaintext.PutLong(temporaryBindingMessageID)
	plaintext.PutInt32(0)
	plaintext.PutInt32(int32(payload.Len()))
	plaintext.Put(payload.Buf)
	digest := sha1.Sum(plaintext.Buf) // #nosec G401 -- MTProto v1 binding format.
	var messageKey gotdbin.Int128
	copy(messageKey[:], digest[4:20])

	padding := make([]byte, aes.BlockSize-len(plaintext.Buf)%aes.BlockSize)
	if _, err := rand.Read(padding); err != nil {
		t.Fatal(err)
	}
	plaintext.Put(padding)

	key, iv := gotdcrypto.OldKeys(permanent, messageKey, gotdcrypto.Client)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := make([]byte, len(plaintext.Buf))
	ige.EncryptBlocks(block, iv[:], ciphertext, plaintext.Buf)

	envelope := &gotdbin.Buffer{}
	if err := (gotdcrypto.EncryptedMessage{
		AuthKeyID:     permanent.ID(),
		MsgKey:        messageKey,
		EncryptedData: ciphertext,
	}).Encode(envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Copy()
}

func (m *expiredStoredKeyManager) TemporaryInfo(id tlrpccrypto.KeyID) (tlrpccrypto.TemporaryAuthKeyInfo, bool, error) {
	if id == m.temporaryID {
		return tlrpccrypto.TemporaryAuthKeyInfo{ExpiresAt: m.expiresAt}, true, nil
	}
	return m.MemoryAuthKeyManager.TemporaryInfo(id)
}

func (m *expiredStoredKeyManager) BindTemporary(_, _ tlrpccrypto.KeyID, _ time.Time) error {
	m.bindCalled = true
	return nil
}

func randomGotdKey(t *testing.T) gotdcrypto.Key {
	t.Helper()
	var key gotdcrypto.Key
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatal(err)
	}
	return key
}

func requireTemporaryBindingError(t *testing.T, err error, message string) {
	t.Helper()
	rpcErr, ok := IsRPCError(err)
	if !ok || rpcErr.ErrorCode != int32(BadRequest) || rpcErr.ErrorMessage != message {
		t.Fatalf("BindTemporaryAuthKey() error = %v, want RPC_ERROR 400: %s", err, message)
	}
}

func requireTemporaryKeyUnbound(t *testing.T, fixture temporaryBindingFixture, wantExpiry time.Time) {
	t.Helper()
	info, temporary, err := fixture.manager.TemporaryInfo(fixture.temporaryID)
	if err != nil || !temporary {
		t.Fatalf("TemporaryInfo() = %#v, %t, %v", info, temporary, err)
	}
	if info.PermanentKeyID != 0 || !info.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("temporary info = %#v, want unbound with expiry %v", info, wantExpiry)
	}
}
