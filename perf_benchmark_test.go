package tlrpc

import (
	"context"
	"strings"
	"testing"

	runtimev2 "github.com/r6m/tlrpc/internal/runtime"
	"github.com/r6m/tlrpc/mtproto"
)

func BenchmarkRuntimeApplicationDispatch(b *testing.B) {
	for _, size := range []struct {
		name string
		n    int
	}{
		{name: "small", n: 16},
		{name: "1KiB", n: 1 << 10},
		{name: "64KiB", n: 64 << 10},
	} {
		b.Run(size.name, func(b *testing.B) {
			server := NewServer()
			registerRuntimeApplicationTestService(server, &runtimeApplicationTestServiceImpl{
				call: func(_ context.Context, request *runtimeApplicationTestRequest) (*runtimeApplicationTestResponse, error) {
					return &runtimeApplicationTestResponse{Value: request.Value}, nil
				},
			})
			dispatcher := newRuntimeApplicationDispatcher(server)
			body, err := encodeTLObject(&runtimeApplicationTestRequest{Value: strings.Repeat("x", size.n)})
			if err != nil {
				b.Fatalf("encode request: %v", err)
			}
			request := runtimeApplicationRequestFromBody(body)

			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				outcome, err := dispatcher.DispatchApplication(context.Background(), request)
				if err != nil {
					b.Fatalf("dispatch application: %v", err)
				}
				if len(outcome.Intents) != 1 {
					b.Fatalf("dispatch intents = %#v, want one RPC result", outcome.Intents)
				}
				if _, ok := outcome.Intents[0].(runtimev2.RPCResult); !ok {
					b.Fatalf("dispatch intent = %T, want runtime RPCResult", outcome.Intents[0])
				}
			}
		})
	}
}

func BenchmarkEncodeMethodResponse(b *testing.B) {
	b.Run("scalar", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := EncodeTypedResponse(int32(42), 0, EncodeLimits{}, func(encoder *mtproto.Encoder, value int32) error {
				return encoder.WriteInt32(value)
			}); err != nil {
				b.Fatalf("encode scalar: %v", err)
			}
		}
	})

	b.Run("vector", func(b *testing.B) {
		value := []int32{1, 2, 3, 4, 5, 6, 7, 8}
		b.SetBytes(int64(len(value) * 4))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := EncodeTypedResponse(value, 0, EncodeLimits{}, encodeInt32VectorResponse); err != nil {
				b.Fatalf("encode vector: %v", err)
			}
		}
	})

	b.Run("nested_vector", func(b *testing.B) {
		value := [][]byte{{1, 2, 3, 4}, {5, 6, 7, 8}, {9, 10, 11, 12}}
		b.SetBytes(12)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := EncodeTypedResponse(value, 0, EncodeLimits{}, encodeNestedBytesVectorResponse); err != nil {
				b.Fatalf("encode nested vector: %v", err)
			}
		}
	})

	for _, size := range []struct {
		name string
		n    int
	}{
		{name: "small", n: 16},
		{name: "1KiB", n: 1 << 10},
		{name: "64KiB", n: 64 << 10},
	} {
		b.Run("bytes/"+size.name, func(b *testing.B) {
			value := []byte(strings.Repeat("x", size.n))
			b.SetBytes(int64(len(value)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := EncodeTypedResponse(value, 0, EncodeLimits{}, func(encoder *mtproto.Encoder, value []byte) error {
					return encoder.WriteBytes(value)
				}); err != nil {
					b.Fatalf("encode bytes: %v", err)
				}
			}
		})
	}
}

func encodeInt32VectorResponse(encoder *mtproto.Encoder, values []int32) error {
	if err := encoder.EnterObject(); err != nil {
		return err
	}
	defer encoder.LeaveObject()
	if err := encoder.WriteVectorHeader(len(values)); err != nil {
		return err
	}
	for _, value := range values {
		if err := encoder.WriteInt32(value); err != nil {
			return err
		}
	}
	return nil
}

func encodeNestedBytesVectorResponse(encoder *mtproto.Encoder, values [][]byte) error {
	if err := encoder.EnterObject(); err != nil {
		return err
	}
	defer encoder.LeaveObject()
	if err := encoder.WriteVectorHeader(len(values)); err != nil {
		return err
	}
	for _, value := range values {
		if err := encoder.WriteBytes(value); err != nil {
			return err
		}
	}
	return nil
}

func runtimeApplicationRequestFromBody(body []byte) runtimev2.Request {
	return runtimev2.Request{
		Message: runtimev2.InboundMessage{
			MessageID:      1,
			SequenceNo:     1,
			ConstructorID:  runtimeApplicationRequestID,
			Body:           body,
			ContentRelated: true,
		},
		Info: runtimev2.RequestInfo{Layer: 37},
	}
}
