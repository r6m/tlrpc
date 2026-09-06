package runtime

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/r6m/tlrpc/crypto"
)

func TestConnectionWritesAuthKeyNotFoundTransportErrorBeforeClose(t *testing.T) {
	const missingKeyID = crypto.KeyID(0x0102030405060708)
	harness := newConnectionHarness(t, time.Now(), &connectionApplicationStub{}, 1, nil)
	harness.transport.frames = [][]byte{encryptedFrameHeader(missingKeyID)}
	harness.connection.config.AuthKeys = authKeySourceFunc(func(keyID crypto.KeyID) (crypto.AuthKey, error) {
		if keyID != missingKeyID {
			t.Fatalf("Get key ID = %d, want %d", keyID, missingKeyID)
		}
		return crypto.AuthKey{}, crypto.ErrAuthKeyNotFound
	})

	err := harness.connection.Run(context.Background())
	if !errors.Is(err, crypto.ErrAuthKeyNotFound) {
		t.Fatalf("Run() error = %v, want auth key not found", err)
	}
	writes := harness.transport.writtenFrames()
	if len(writes) != 1 || len(writes[0]) != 4 {
		t.Fatalf("written frames = %x, want one four-byte transport error", writes)
	}
	if code := int32(binary.LittleEndian.Uint32(writes[0])); code != transportErrorAuthKeyNotFound {
		t.Fatalf("transport error = %d, want %d", code, transportErrorAuthKeyNotFound)
	}
	if events := harness.transport.recordedEvents(); !reflect.DeepEqual(events, []string{"write", "close"}) {
		t.Fatalf("transport events = %v, want write before close", events)
	}
}

func TestConnectionDoesNotWriteTransportErrorForAuthKeySourceFailure(t *testing.T) {
	const keyID = crypto.KeyID(0x0102030405060708)
	storeErr := errors.New("database temporarily unavailable")
	harness := newConnectionHarness(t, time.Now(), &connectionApplicationStub{}, 0, nil)
	harness.transport.frames = [][]byte{encryptedFrameHeader(keyID)}
	harness.connection.config.AuthKeys = authKeySourceFunc(func(crypto.KeyID) (crypto.AuthKey, error) {
		return crypto.AuthKey{}, storeErr
	})

	err := harness.connection.Run(context.Background())
	if !errors.Is(err, storeErr) {
		t.Fatalf("Run() error = %v, want storage failure", err)
	}
	if writes := harness.transport.writtenFrames(); len(writes) != 0 {
		t.Fatalf("written frames = %x, want none", writes)
	}
	if events := harness.transport.recordedEvents(); !reflect.DeepEqual(events, []string{"close"}) {
		t.Fatalf("transport events = %v, want close only", events)
	}
}

func encryptedFrameHeader(keyID crypto.KeyID) []byte {
	frame := make([]byte, 24)
	binary.LittleEndian.PutUint64(frame, uint64(keyID))
	return frame
}

type authKeySourceFunc func(crypto.KeyID) (crypto.AuthKey, error)

func (f authKeySourceFunc) Get(keyID crypto.KeyID) (crypto.AuthKey, error) {
	return f(keyID)
}
