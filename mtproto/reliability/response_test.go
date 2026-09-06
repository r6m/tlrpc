package reliability

import (
	"testing"
	"time"
)

func TestLookupResponseUsesBoundedRetention(t *testing.T) {
	now := time.Unix(100, 0)
	store, err := New(Config{Capacity: 2, TTL: time.Second, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	put := func(id, request int64) {
		t.Helper()
		if _, err := store.Put(SentMessage{MessageID: id, RequestMessageID: request, Payload: []byte{1, 2}, SentAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	put(5, 4)
	put(9, 8)
	got, ok := store.LookupResponse(4)
	if !ok || got.MessageID != 5 {
		t.Fatalf("lookup=%+v %v", got, ok)
	}
	got.Payload[0] = 9
	again, _ := store.LookupResponse(4)
	if again.Payload[0] != 1 {
		t.Fatal("payload alias")
	}
	store.Acknowledge(5)
	got, _ = store.LookupResponse(4)
	if !got.Acknowledged {
		t.Fatal("lost acknowledgement")
	}
	put(13, 12)
	if _, ok := store.LookupResponse(4); ok {
		t.Fatal("evicted response retained")
	}
	now = now.Add(time.Second)
	if _, ok := store.LookupResponse(8); ok {
		t.Fatal("expired response retained")
	}
	if _, ok := store.LookupResponse(0); ok {
		t.Fatal("uncorrelated packet returned")
	}
}
