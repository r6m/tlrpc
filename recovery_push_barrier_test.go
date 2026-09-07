package tlrpc

import "testing"

func TestWithRecoveryPushBarrierCopiesAndCombinesMethodPolicy(t *testing.T) {
	first := []uint32{0x10101010}
	server := NewServer(
		WithRecoveryPushBarrier(first...),
		WithRecoveryPushBarrier(0x20202020, 0x10101010),
	)
	first[0] = 0
	if len(server.recoveryPushBarrierMethods) != 2 {
		t.Fatalf("barrier methods = %v, want two", server.recoveryPushBarrierMethods)
	}
	for _, constructorID := range []uint32{0x10101010, 0x20202020} {
		if _, ok := server.recoveryPushBarrierMethods[constructorID]; !ok {
			t.Fatalf("method 0x%08x is not protected", constructorID)
		}
	}
}

func TestWithRecoveryPushBarrierRejectsInvalidPolicy(t *testing.T) {
	for _, ids := range [][]uint32{nil, {0}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("accepted invalid method IDs %v", ids)
				}
			}()
			_ = WithRecoveryPushBarrier(ids...)
		}()
	}
}
