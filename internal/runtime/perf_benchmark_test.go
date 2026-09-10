package runtime

import (
	"testing"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
)

func BenchmarkDecodeEncryptedFrame(b *testing.B) {
	var authKey crypto.AuthKey
	for i := range authKey {
		authKey[i] = byte(i + 1)
	}
	keys := authKeyMap{authKey.ID(): authKey}

	for _, size := range []struct {
		name string
		n    int
	}{
		{name: "small", n: 16},
		{name: "1KiB", n: 1 << 10},
		{name: "64KiB", n: 64 << 10},
	} {
		b.Run(size.name, func(b *testing.B) {
			data := make([]byte, size.n)
			for i := range data {
				data[i] = byte(i)
			}
			encrypted, err := (&mtproto.InnerData{
				Salt: 7, SessionID: 9, MsgID: 12, SeqNo: 1, Data: data,
			}).EncryptFromClient(authKey, authKey.ID())
			if err != nil {
				b.Fatalf("encrypt fixture: %v", err)
			}
			frame := serializeEncryptedFrame(encrypted)

			b.SetBytes(int64(len(frame)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := DecodeFrame(frame, keys); err != nil {
					b.Fatalf("decode frame: %v", err)
				}
			}
		})
	}
}
