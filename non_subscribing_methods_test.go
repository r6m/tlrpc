package tlrpc

import "testing"

func TestWithNonSubscribingMethodsCopiesAndCombinesPolicy(t *testing.T) {
	first := []uint32{0x41414141}
	server := NewServer(
		WithNonSubscribingMethods(first...),
		WithNonSubscribingMethods(0x42424242, 0x41414141),
	)
	first[0] = 0
	if len(server.nonSubscribingMethods) != 2 {
		t.Fatalf("non-subscribing methods = %v, want two", server.nonSubscribingMethods)
	}
	for _, constructorID := range []uint32{0x41414141, 0x42424242} {
		if _, ok := server.nonSubscribingMethods[constructorID]; !ok {
			t.Fatalf("method 0x%08x is not configured", constructorID)
		}
	}
}

func TestWithNonSubscribingMethodsRejectsInvalidPolicy(t *testing.T) {
	for _, ids := range [][]uint32{nil, {0}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("accepted invalid constructor IDs %v", ids)
				}
			}()
			_ = WithNonSubscribingMethods(ids...)
		}()
	}
}
