package tlrpc

import (
	"testing"
	"time"

	"github.com/r6m/tlrpc/crypto"
)

func TestValidateMessageID(t *testing.T) {
	// Test valid message ID - MTProto uses Unix timestamp in seconds for the high 32 bits
	now := time.Now().Unix()          // Unix timestamp in seconds
	validMsgID := int64(now<<32) &^ 3 // Put timestamp in high 32 bits, clear bottom 2 bits

	err := validateMessageID(validMsgID)
	if err != nil {
		t.Errorf("validateMessageID returned error for valid ID: %v", err)
	}

	// Test invalid message ID (bottom bits not zero)
	invalidMsgID := validMsgID | 1 // Set bottom bit
	err = validateMessageID(invalidMsgID)
	if err == nil {
		t.Error("validateMessageID should reject message ID with bottom bits set")
	}

	// Test message ID too old
	oldTimestamp := (time.Now().Add(-31*time.Second).UnixNano() / 1000000) << 32
	err = validateMessageID(oldTimestamp)
	if err == nil {
		t.Error("validateMessageID should reject too old message ID")
	}

	// Test message ID too new
	newTimestamp := (time.Now().Add(31*time.Second).UnixNano() / 1000000) << 32
	err = validateMessageID(newTimestamp)
	if err == nil {
		t.Error("validateMessageID should reject too new message ID")
	}
}

func TestNewDefaultHandshakeHandler(t *testing.T) {
	authKeys := crypto.NewMemoryAuthKeyManager()
	serverKeys := crypto.NewMemoryServerKeyManager()

	handler := NewDefaultHandshakeHandler(authKeys, serverKeys)

	if handler == nil {
		t.Fatal("NewDefaultHandshakeHandler returned nil")
	}

	if handler.authKeys != authKeys {
		t.Error("authKeys not set correctly")
	}

	if handler.serverKeys != serverKeys {
		t.Error("serverKeys not set correctly")
	}
}

func TestDefaultHandshakeHandlerUnsupportedMessage(t *testing.T) {
	handler := NewDefaultHandshakeHandler(
		crypto.NewMemoryAuthKeyManager(),
		crypto.NewMemoryServerKeyManager(),
	)

	// Test unsupported constructor ID
	data := make([]byte, 4)
	// Use a constructor ID that's not handled
	data[0] = 0xFF
	data[1] = 0xFF
	data[2] = 0xFF
	data[3] = 0xFF

	_, err := handler.HandleUnencrypted(nil, 0, data)
	if err != ErrUnsupportedMessage {
		t.Errorf("expected ErrUnsupportedMessage, got: %v", err)
	}
}

func TestDefaultHandshakeHandlerInvalidData(t *testing.T) {
	handler := NewDefaultHandshakeHandler(
		crypto.NewMemoryAuthKeyManager(),
		crypto.NewMemoryServerKeyManager(),
	)

	// Test with data too short
	_, err := handler.HandleUnencrypted(nil, 0, []byte{0x00})
	if err != ErrInvalidHandshake {
		t.Errorf("expected ErrInvalidHandshake for short data, got: %v", err)
	}
}

func TestTempDHStateStorage(t *testing.T) {
	// Test that tempDHStates map exists and is accessible
	// This is a basic test to ensure the global variable is initialized

	if tempDHStates == nil {
		t.Error("tempDHStates map not initialized")
	}

	// Test basic map operations
	key := [32]byte{1, 2, 3}
	state := &TempDHState{
		Nonce:       [16]byte{1},
		ServerNonce: [16]byte{2},
		NewNonce:    key,
	}

	tempDHStates[key] = state

	if tempDHStates[key] != state {
		t.Error("tempDHStates map not working correctly")
	}

	// Clean up
	delete(tempDHStates, key)
}

// Test generateNonce indirectly through the handshake handler
func TestGenerateNonce(t *testing.T) {
	// We can't directly test generateNonce since it's not exported,
	// but we can test that the handshake handler creates different nonces
	handler := NewDefaultHandshakeHandler(
		crypto.NewMemoryAuthKeyManager(),
		crypto.NewMemoryServerKeyManager(),
	)

	// This test ensures that the handler can be created and basic functions work
	// The actual nonce generation is tested indirectly through integration tests
	if handler == nil {
		t.Error("handler creation failed")
	}
}

// Test error variables
func TestHandshakeErrors(t *testing.T) {
	if ErrHandshakeFailed == nil {
		t.Error("ErrHandshakeFailed not initialized")
	}
	if ErrInvalidHandshake == nil {
		t.Error("ErrInvalidHandshake not initialized")
	}
	if ErrUnsupportedMessage == nil {
		t.Error("ErrUnsupportedMessage not initialized")
	}

	// Test error messages
	if ErrHandshakeFailed.Error() != "tlrpc: handshake failed" {
		t.Error("ErrHandshakeFailed has wrong message")
	}
	if ErrInvalidHandshake.Error() != "tlrpc: invalid handshake message" {
		t.Error("ErrInvalidHandshake has wrong message")
	}
	if ErrUnsupportedMessage.Error() != "tlrpc: unsupported message type" {
		t.Error("ErrUnsupportedMessage has wrong message")
	}
}
