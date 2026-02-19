package mtproto

import (
	"testing"
	"time"
)

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

func TestMsgIDGeneratorMonotonicWhenTimeBackwards(t *testing.T) {
	gen := NewMsgIDGenerator()
	times := []int64{10_000_000_000, 9_000_000_000, 8_000_000_000}
	idx := 0
	gen.now = func() time.Time {
		t := time.Unix(0, times[idx])
		if idx < len(times)-1 {
			idx++
		}
		return t
	}

	first := gen.nextServerMsgID(msgIDResponse)
	second := gen.nextServerMsgID(msgIDResponse)
	if second <= first {
		t.Fatalf("msg_id not monotonic when time goes backwards: %d then %d", first, second)
	}
}
