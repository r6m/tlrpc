package tlrpc

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/r6m/tlrpc/mtproto"
)

func TestEncodeTypedResponseChecksDeclaredResult(t *testing.T) {
	encode := func(encoder *mtproto.Encoder, value *runtimeApplicationTestResponse) error {
		if value == nil {
			return fmt.Errorf("nil response")
		}
		return value.SerializeTL(encoder)
	}
	if _, err := EncodeTypedResponse(&runtimeApplicationTestRequest{}, 228, EncodeLimits{}, encode); err == nil {
		t.Fatal("accepted the wrong concrete result")
	}
	if _, err := EncodeTypedResponse((*runtimeApplicationTestResponse)(nil), 228, EncodeLimits{}, encode); err == nil || err.Error() != "nil response" {
		t.Fatalf("typed nil callback error = %v", err)
	}
	if _, err := EncodeTypedResponse("too large", 228, EncodeLimits{MaxEncodedBytes: 4}, func(encoder *mtproto.Encoder, value string) error {
		return encoder.WriteString(value)
	}); !errors.Is(err, ErrEncodedTLTooLarge) {
		t.Fatalf("byte limit error = %v", err)
	}
}

func TestEncodeTypedResponsePreservesPrimitiveAndVectorWireShapes(t *testing.T) {
	intBytes, err := EncodeTypedResponse(int32(42), 228, EncodeLimits{}, func(encoder *mtproto.Encoder, value int32) error {
		return encoder.WriteInt32(value)
	})
	if err != nil || len(intBytes) != 4 || binary.LittleEndian.Uint32(intBytes) != 42 {
		t.Fatalf("int encoding = %x, %v", intBytes, err)
	}
	flags, err := EncodeTypedResponse(uint32(0xffffffff), 228, EncodeLimits{}, func(encoder *mtproto.Encoder, value uint32) error {
		return encoder.WriteUint32(value)
	})
	if err != nil || !bytes.Equal(flags, []byte{255, 255, 255, 255}) {
		t.Fatalf("flags encoding = %x, %v", flags, err)
	}
	longBytes, err := EncodeTypedResponse(int64(1<<40), 228, EncodeLimits{}, func(encoder *mtproto.Encoder, value int64) error {
		return encoder.WriteInt64(value)
	})
	if err != nil || len(longBytes) != 8 || binary.LittleEndian.Uint64(longBytes) != 1<<40 {
		t.Fatalf("long encoding = %x, %v", longBytes, err)
	}
	vector, err := EncodeTypedResponse([]int32{1, 2}, 228, EncodeLimits{}, func(encoder *mtproto.Encoder, values []int32) error {
		if err := encoder.WriteVectorHeader(len(values)); err != nil {
			return err
		}
		for _, value := range values {
			if err := encoder.WriteInt32(value); err != nil {
				return err
			}
		}
		return nil
	})
	want := []byte{0x15, 0xc4, 0xb5, 0x1c, 2, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0}
	if err != nil || !bytes.Equal(vector, want) {
		t.Fatalf("vector encoding = %x, %v", vector, err)
	}
}

func TestRuntimeApplicationRejectsInterceptorResultOutsideDeclaredContract(t *testing.T) {
	server := NewServer(WithUnaryInterceptor(func(context.Context, any, *UnaryServerInfo, UnaryHandler) (any, error) {
		return &runtimeApplicationTestRequest{}, nil
	}))
	registerRuntimeApplicationTestService(server, &runtimeApplicationTestServiceImpl{call: func(context.Context, *runtimeApplicationTestRequest) (*runtimeApplicationTestResponse, error) {
		t.Fatal("interceptor should replace handler result")
		return nil, nil
	}})
	outcome, err := newRuntimeApplicationDispatcher(server).DispatchApplication(context.Background(), runtimeApplicationRequest(t, 101, "request"))
	if err != nil {
		t.Fatal(err)
	}
	rpcErr := requireRuntimeApplicationError(t, outcome)
	if rpcErr.RequestMessageID != 101 || rpcErr.Code != 500 || rpcErr.Message != "RESPONSE_ENCODE_FAILED" {
		t.Fatalf("wrong correlated error: %+v", rpcErr)
	}
}

func TestRuntimeApplicationRejectsInterceptorRequestOutsideDeclaredContract(t *testing.T) {
	server := NewServer(WithUnaryInterceptor(func(ctx context.Context, _ any, _ *UnaryServerInfo, handler UnaryHandler) (any, error) {
		return handler(ctx, &runtimeApplicationTestResponse{})
	}))
	registerRuntimeApplicationTestService(server, &runtimeApplicationTestServiceImpl{call: func(context.Context, *runtimeApplicationTestRequest) (*runtimeApplicationTestResponse, error) {
		t.Fatal("wrong interceptor request reached the typed service")
		return nil, nil
	}})
	outcome, err := newRuntimeApplicationDispatcher(server).DispatchApplication(context.Background(), runtimeApplicationRequest(t, 102, "request"))
	if err != nil {
		t.Fatal(err)
	}
	rpcErr := requireRuntimeApplicationError(t, outcome)
	if rpcErr.RequestMessageID != 102 || rpcErr.Code != 500 || rpcErr.Message != "INTERNAL" {
		t.Fatalf("wrong correlated error: %+v", rpcErr)
	}
}

func TestRuntimeApplicationChecksEncodedCallbackSize(t *testing.T) {
	server := NewServer()
	server.maxEncodedResponseBytes = 4
	registerRuntimeApplicationTestService(server, &runtimeApplicationTestServiceImpl{call: func(context.Context, *runtimeApplicationTestRequest) (*runtimeApplicationTestResponse, error) {
		return &runtimeApplicationTestResponse{}, nil
	}})
	dispatcher := newRuntimeApplicationDispatcher(server)
	for id, methods := range dispatcher.methods {
		methods[0].descriptor.EncodeResponse = func(any, int, EncodeLimits) ([]byte, error) {
			return make([]byte, 5), nil
		}
		dispatcher.methods[id] = methods
	}
	outcome, err := dispatcher.DispatchApplication(context.Background(), runtimeApplicationRequest(t, 101, "request"))
	if err != nil {
		t.Fatal(err)
	}
	if got := requireRuntimeApplicationError(t, outcome); got.Message != "RESPONSE_ENCODE_FAILED" {
		t.Fatalf("oversized encoder result: %+v", got)
	}
}
