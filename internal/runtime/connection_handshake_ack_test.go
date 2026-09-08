package runtime

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/r6m/tlrpc/internal/handshake"
	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
)

func TestPlaintextHandshakeAcknowledgementBoundary(t *testing.T) {
	body, err := serializeRuntimeTL(&mtprototl.MsgsAck{MsgIDs: []int64{101, 105}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                                     string
		started, completed, pinned, sessionBound bool
		wantError                                bool
	}{
		{name: "before_handshake", wantError: true},
		{name: "during_handshake", started: true},
		{name: "after_dh_gen_ok", started: true, completed: true},
		{name: "encrypted_key_pinned", started: true, completed: true, pinned: true, wantError: true},
		{name: "encrypted_session_bound", started: true, sessionBound: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Connection{authKeyPinned: tc.pinned}
			if tc.started {
				c.handshakeSession = &handshake.Session{}
			}
			if tc.completed {
				c.authorization = &handshake.Result{AuthKeyID: 42, InitialServerSalt: 99}
			}
			if tc.sessionBound {
				c.sessions = map[session.SessionKey]*connectionSession{{SessionID: 1}: nil}
			}
			beforeSession, beforeAuthorization := c.handshakeSession, c.authorization
			// No transport/reliability dependencies: a valid ACK must not write
			// a response, advance key exchange, or touch session reliability.
			for i := 0; i < 2; i++ {
				err := c.handleUnencrypted(context.Background(), &mtproto.UnencryptedMessage{Data: body})
				if tc.wantError {
					if !errors.Is(err, ErrConnectionProtocol) {
						t.Fatalf("ACK error = %v, want ErrConnectionProtocol", err)
					}
				} else if err != nil {
					t.Fatalf("ACK: %v", err)
				}
			}
			if c.handshakeSession != beforeSession || c.authorization != beforeAuthorization {
				t.Fatal("ACK replaced handshake state")
			}
		})
	}
}

func TestMalformedPlaintextHandshakeAcknowledgements(t *testing.T) {
	valid, err := serializeRuntimeTL(&mtprototl.MsgsAck{MsgIDs: []int64{101}})
	if err != nil {
		t.Fatal(err)
	}
	wrongVector := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint32(wrongVector[4:], 0)
	negativeCount := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint32(negativeCount[8:], 0xffffffff)
	oversizedCount := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint32(oversizedCount[8:], uint32(mtprototl.MaxMessageStateIDs+1))
	for name, body := range map[string][]byte{
		"missing_vector":  valid[:4],
		"wrong_vector":    wrongVector,
		"negative_count":  negativeCount,
		"oversized_count": oversizedCount,
		"truncated_id":    valid[:len(valid)-1],
		"trailing_bytes":  append(append([]byte(nil), valid...), 0, 0, 0, 0),
	} {
		t.Run(name, func(t *testing.T) {
			c := &Connection{handshakeSession: &handshake.Session{}}
			if err := c.handleUnencrypted(context.Background(), &mtproto.UnencryptedMessage{Data: body}); err == nil {
				t.Fatal("malformed ACK accepted")
			}
		})
	}
}

func TestCompletedHandshakeStillRejectsOtherPlaintextMessages(t *testing.T) {
	c := &Connection{handshakeSession: &handshake.Session{}, authorization: &handshake.Result{AuthKeyID: 42}}
	for _, body := range [][]byte{nil, {1, 2, 3}, constructorBody(0xbe7e8ef1)} {
		if err := c.handleUnencrypted(context.Background(), &mtproto.UnencryptedMessage{Data: body}); !errors.Is(err, ErrConnectionProtocol) {
			t.Fatalf("plaintext %x error = %v, want ErrConnectionProtocol", body, err)
		}
	}
}
