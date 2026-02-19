package mtproto

import "testing"

func TestMsgIDGeneratorMonotonic(t *testing.T) {
	gen := NewMsgIDGenerator()
	id1 := gen.Next()
	id2 := gen.Next()
	if id2 <= id1 {
		t.Fatalf("msg_id not monotonic: %d then %d", id1, id2)
	}
	if id1%4 != 0 || id2%4 != 0 {
		t.Fatalf("msg_id not 4-aligned")
	}
}
