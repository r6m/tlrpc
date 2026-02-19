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

func TestMsgIDGeneratorServerBits(t *testing.T) {
	gen := NewMsgIDGenerator()
	resp := gen.nextServerMsgID(msgIDResponse)
	push := gen.nextServerMsgID(msgIDPush)

	if resp%4 != 1 {
		t.Fatalf("response msg_id mod 4 = %d, want 1", resp%4)
	}
	if push%4 != 3 {
		t.Fatalf("push msg_id mod 4 = %d, want 3", push%4)
	}
	if push <= resp {
		t.Fatalf("push msg_id not monotonic: resp=%d push=%d", resp, push)
	}
}
