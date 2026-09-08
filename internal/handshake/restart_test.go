package handshake

import (
	"context"
	"testing"
	"time"
)

func TestRestartUnfinishedHandshake(t *testing.T) {
	engine, keys := newTestEngine(t, 1, time.Minute, time.Now)
	session, err := engine.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	advanceToClientDH(t, session, 1)
	// A fresh req_pq on the same socket must abandon the previous nonces/DH.
	result := completeHandshake(t, session, 2)
	if _, err := keys.Get(result.AuthKeyID); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Handle(context.Background(), 0, encodeReqPQ([16]byte{3})); err == nil {
		t.Fatal("completed engine session reused; runtime must allocate a fresh session")
	}
}
