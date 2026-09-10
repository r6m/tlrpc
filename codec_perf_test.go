package tlrpc_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	echo "github.com/r6m/tlrpc/examples/echo/gen"
	gen "github.com/r6m/tlrpc/examples/gen"
	"github.com/r6m/tlrpc/mtproto"
)

func BenchmarkGeneratedEchoCodec(b *testing.B) {
	for _, size := range []struct {
		name string
		n    int
	}{
		{name: "small", n: 16},
		{name: "1KiB", n: 1 << 10},
		{name: "64KiB", n: 64 << 10},
	} {
		b.Run("encode/"+size.name, func(b *testing.B) {
			request := &echo.EchoEchoRequest{Message: strings.Repeat("x", size.n)}
			var buffer bytes.Buffer
			b.SetBytes(int64(size.n))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buffer.Reset()
				if err := request.SerializeTL(&buffer); err != nil {
					b.Fatalf("serialize echo request: %v", err)
				}
			}
		})

		b.Run("decode/"+size.name, func(b *testing.B) {
			encoded := encodeGeneratedEchoRequest(b, strings.Repeat("x", size.n))
			b.SetBytes(int64(len(encoded)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var request echo.EchoEchoRequest
				if err := request.DeserializeTL(bytes.NewReader(encoded)); err != nil {
					b.Fatalf("deserialize echo request: %v", err)
				}
			}
		})
	}
}

func BenchmarkGeneratedNestedBoxedVectorCodec(b *testing.B) {
	value := &gen.UpdateMessagePollVote{
		PollID: 1,
		Peer:   &gen.PeerUser{UserID: 2},
		Options: [][]byte{
			[]byte("first"),
			[]byte("second"),
			[]byte("third"),
		},
		Qts: 3,
	}
	encoded := encodeGeneratedNestedBoxedVector(b, value)

	b.Run("encode", func(b *testing.B) {
		var buffer bytes.Buffer
		b.SetBytes(int64(len(encoded)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buffer.Reset()
			if err := value.SerializeTL(&buffer); err != nil {
				b.Fatalf("serialize nested generated value: %v", err)
			}
		}
	})

	b.Run("decode", func(b *testing.B) {
		b.SetBytes(int64(len(encoded)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var decoded gen.UpdateMessagePollVote
			if err := decoded.DeserializeTL(bytes.NewReader(encoded)); err != nil {
				b.Fatalf("deserialize nested generated value: %v", err)
			}
		}
	})
}

func BenchmarkGeneratedEchoCodecBufferPath(b *testing.B) {
	for _, size := range []struct {
		name string
		n    int
	}{
		{name: "small", n: 16},
		{name: "1KiB", n: 1 << 10},
		{name: "64KiB", n: 64 << 10},
	} {
		b.Run("encode/"+size.name, func(b *testing.B) {
			request := &echo.EchoEchoRequest{Message: strings.Repeat("x", size.n)}
			b.SetBytes(int64(size.n))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				encoder, err := mtproto.NewBufferEncoder(0, 0)
				if err != nil {
					b.Fatalf("new buffer encoder: %v", err)
				}
				if err := request.SerializeTL(encoder); err != nil {
					b.Fatalf("encode echo request: %v", err)
				}
			}
		})

		b.Run("decode/"+size.name, func(b *testing.B) {
			encoded := encodeGeneratedEchoRequest(b, strings.Repeat("x", size.n))
			b.SetBytes(int64(len(encoded)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var request echo.EchoEchoRequest
				if err := request.DeserializeTL(mtproto.NewDecoderBytes(encoded, 0, nil)); err != nil {
					b.Fatalf("decode echo request: %v", err)
				}
			}
		})
	}
}

func BenchmarkGeneratedNestedBoxedVectorCodecBufferPath(b *testing.B) {
	value := &gen.UpdateMessagePollVote{
		PollID:  1,
		Peer:    &gen.PeerUser{UserID: 2},
		Options: [][]byte{[]byte("first"), []byte("second"), []byte("third")},
		Qts:     3,
	}
	encoded := encodeGeneratedNestedBoxedVector(b, value)

	b.Run("encode", func(b *testing.B) {
		b.SetBytes(int64(len(encoded)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			encoder, err := mtproto.NewBufferEncoder(0, 0)
			if err != nil {
				b.Fatalf("new buffer encoder: %v", err)
			}
			if err := value.SerializeTL(encoder); err != nil {
				b.Fatalf("encode nested generated value: %v", err)
			}
		}
	})

	b.Run("decode", func(b *testing.B) {
		b.SetBytes(int64(len(encoded)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var decoded gen.UpdateMessagePollVote
			if err := decoded.DeserializeTL(mtproto.NewDecoderBytes(encoded, 0, nil)); err != nil {
				b.Fatalf("decode nested generated value: %v", err)
			}
		}
	})
}

func BenchmarkGeneratedEchoCallback(b *testing.B) {
	server := benchmarkEchoServer{}
	request := &echo.EchoEchoRequest{Message: "benchmark"}
	handler := echo.Echo_ServiceDesc.Methods[0].Handler
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := handler(server, ctx, request); err != nil {
			b.Fatalf("invoke generated callback: %v", err)
		}
	}
}

type benchmarkEchoServer struct {
	echo.UnimplementedEchoServer
}

func (benchmarkEchoServer) Echo(_ context.Context, request *echo.EchoEchoRequest) (*echo.EchoResponse, error) {
	return &echo.EchoResponse{Message: request.Message}, nil
}

func encodeGeneratedEchoRequest(b *testing.B, message string) []byte {
	b.Helper()
	var buffer bytes.Buffer
	if err := (&echo.EchoEchoRequest{Message: message}).SerializeTL(&buffer); err != nil {
		b.Fatalf("encode echo fixture: %v", err)
	}
	return buffer.Bytes()
}

func encodeGeneratedNestedBoxedVector(b *testing.B, value *gen.UpdateMessagePollVote) []byte {
	b.Helper()
	var buffer bytes.Buffer
	if err := value.SerializeTL(&buffer); err != nil {
		b.Fatalf("encode nested generated fixture: %v", err)
	}
	return buffer.Bytes()
}
