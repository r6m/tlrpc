package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/r6m/tlrpc/crypto"
)

func TestTemporarySinkExpiresIdleConnectionAndRejectsRevocation(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		t.Run(map[bool]string{false: "expiry", true: "revocation"}[revoked], func(t *testing.T) {
			keys := crypto.NewMemoryAuthKeyManager()
			var parent, temp crypto.AuthKey
			parent[0] = 1
			temp[0] = 2
			if err := keys.Put(parent.ID(), parent); err != nil {
				t.Fatal(err)
			}
			if err := keys.PutTemporary(temp.ID(), temp, time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			conn := newScriptedFrameConnection(nil, 10)
			defer conn.Close()
			owner := &Connection{config: ConnectionConfig{Conn: conn, AuthKeys: keys}, frameSink: newConnectionFrameSink(conn)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sink, err := temporaryKeySink(ctx, owner, temp.ID())
			if err != nil {
				t.Fatal(err)
			}
			if err := keys.BindTemporary(temp.ID(), parent.ID(), time.Now().Add(100*time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			if err := sink.WriteFrame(ctx, []byte{1}); err != nil {
				t.Fatal(err)
			}
			if revoked {
				if err := keys.Delete(parent.ID()); err != nil {
					t.Fatal(err)
				}
				if err := sink.WriteFrame(ctx, []byte{2}); !errors.Is(err, crypto.ErrAuthKeyNotFound) {
					t.Fatalf("revoked write: %v", err)
				}
			} else {
				select {
				case <-conn.Context().Done():
				case <-time.After(time.Second):
					t.Fatal("idle temporary connection stayed open")
				}
			}
		})
	}
}
