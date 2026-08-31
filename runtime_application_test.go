package tlrpc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/r6m/tlrpc/crypto"
	runtimev2 "github.com/r6m/tlrpc/internal/runtime"
	"github.com/r6m/tlrpc/mtproto"
)

const (
	runtimeApplicationRequestID  = uint32(0x71000001)
	runtimeApplicationResponseID = uint32(0x71000002)
)

type runtimeApplicationTestRequest struct {
	Value string
}

func (*runtimeApplicationTestRequest) ConstructorID() uint32 { return runtimeApplicationRequestID }

func (r *runtimeApplicationTestRequest) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, r.ConstructorID()); err != nil {
		return err
	}
	return mtproto.WriteString(w, r.Value)
}

func (r *runtimeApplicationTestRequest) DeserializeTL(rd io.Reader) error {
	constructorID, err := mtproto.ReadUint32(rd)
	if err != nil {
		return err
	}
	if constructorID != r.ConstructorID() {
		return fmt.Errorf("wrong constructor 0x%08x", constructorID)
	}
	r.Value, err = mtproto.ReadString(rd)
	return err
}

type runtimeApplicationTestResponse struct {
	Value string
}

func (*runtimeApplicationTestResponse) ConstructorID() uint32 { return runtimeApplicationResponseID }

func (r *runtimeApplicationTestResponse) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, r.ConstructorID()); err != nil {
		return err
	}
	return mtproto.WriteString(w, r.Value)
}

func (r *runtimeApplicationTestResponse) DeserializeTL(rd io.Reader) error {
	constructorID, err := mtproto.ReadUint32(rd)
	if err != nil {
		return err
	}
	if constructorID != r.ConstructorID() {
		return fmt.Errorf("wrong constructor 0x%08x", constructorID)
	}
	r.Value, err = mtproto.ReadString(rd)
	return err
}

type runtimeApplicationTestService interface {
	Call(context.Context, *runtimeApplicationTestRequest) (*runtimeApplicationTestResponse, error)
}

type runtimeApplicationTestServiceImpl struct {
	call func(context.Context, *runtimeApplicationTestRequest) (*runtimeApplicationTestResponse, error)
}

func (s *runtimeApplicationTestServiceImpl) Call(ctx context.Context, request *runtimeApplicationTestRequest) (*runtimeApplicationTestResponse, error) {
	return s.call(ctx, request)
}

func runtimeApplicationTestHandler(service interface{}, ctx context.Context, request *runtimeApplicationTestRequest) (*runtimeApplicationTestResponse, error) {
	return service.(runtimeApplicationTestService).Call(ctx, request)
}

func registerRuntimeApplicationTestService(server *Server, impl runtimeApplicationTestService) {
	server.RegisterService(ServiceDesc{
		ServiceName: "custom.SchemaService",
		SchemaLayer: 37,
		HandlerType: (*runtimeApplicationTestService)(nil),
		Methods: []MethodDesc{
			{
				MethodName:    "Call",
				ConstructorID: runtimeApplicationRequestID,
				NewRequest: func() TLObject {
					return &runtimeApplicationTestRequest{}
				},
				Handler: runtimeApplicationTestHandler,
			},
		},
	}, impl)
}

func runtimeApplicationRequest(t *testing.T, messageID int64, value string) runtimev2.Request {
	t.Helper()
	body, err := encodeTLObject(&runtimeApplicationTestRequest{Value: value})
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	return runtimev2.Request{
		Message: runtimev2.InboundMessage{
			MessageID:      messageID,
			SequenceNo:     1,
			ConstructorID:  runtimeApplicationRequestID,
			Body:           body,
			ContentRelated: true,
		},
		Info: runtimev2.RequestInfo{
			AuthKeyID: crypto.KeyID(91),
			SessionID: 92,
			UserID:    93,
			Layer:     37,
		},
	}
}

func TestRuntimeApplicationDispatcherCustomSchema(t *testing.T) {
	server := NewServer()
	registerRuntimeApplicationTestService(server, &runtimeApplicationTestServiceImpl{
		call: func(_ context.Context, request *runtimeApplicationTestRequest) (*runtimeApplicationTestResponse, error) {
			return &runtimeApplicationTestResponse{Value: "response:" + request.Value}, nil
		},
	})

	outcome, err := newRuntimeApplicationDispatcher(server).DispatchApplication(
		context.Background(), runtimeApplicationRequest(t, 101, "arbitrary"),
	)
	if err != nil {
		t.Fatalf("dispatch application: %v", err)
	}
	result := requireRuntimeApplicationResult(t, outcome)
	if result.RequestMessageID != 101 {
		t.Fatalf("request message ID = %d, want 101", result.RequestMessageID)
	}
	response := &runtimeApplicationTestResponse{}
	if err := response.DeserializeTL(bytes.NewReader(result.Body)); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Value != "response:arbitrary" {
		t.Fatalf("response value = %q", response.Value)
	}
}

func TestRuntimeApplicationDispatcherMapsHandlerRPCError(t *testing.T) {
	server := NewServer()
	registerRuntimeApplicationTestService(server, &runtimeApplicationTestServiceImpl{
		call: func(context.Context, *runtimeApplicationTestRequest) (*runtimeApplicationTestResponse, error) {
			return nil, NewForbiddenError("CUSTOM_DENIED")
		},
	})

	outcome, err := newRuntimeApplicationDispatcher(server).DispatchApplication(
		context.Background(), runtimeApplicationRequest(t, 102, "denied"),
	)
	if err != nil {
		t.Fatalf("dispatch application: %v", err)
	}
	rpcErr := requireRuntimeApplicationError(t, outcome)
	if rpcErr.RequestMessageID != 102 || rpcErr.Code != 403 || rpcErr.Message != "CUSTOM_DENIED" {
		t.Fatalf("RPC error = %#v", rpcErr)
	}
}

func TestRuntimeApplicationDispatcherIsolatesHandlerPanicAndRemainsUsable(t *testing.T) {
	const secret = "sentinel-handler-panic-secret"
	calls := 0
	server := NewServer()
	registerRuntimeApplicationTestService(server, &runtimeApplicationTestServiceImpl{
		call: func(_ context.Context, request *runtimeApplicationTestRequest) (*runtimeApplicationTestResponse, error) {
			calls++
			if calls == 1 {
				panic(secret)
			}
			return &runtimeApplicationTestResponse{Value: "response:" + request.Value}, nil
		},
	})
	dispatcher := newRuntimeApplicationDispatcher(server)

	outcome, err := dispatcher.DispatchApplication(context.Background(), runtimeApplicationRequest(t, 108, "panic"))
	if err != nil {
		t.Fatalf("dispatch panicking handler: %v", err)
	}
	rpcErr := requireRuntimeApplicationError(t, outcome)
	if rpcErr.Code != int32(Internal) || rpcErr.Message != "INTERNAL" {
		t.Fatalf("panic RPC error = %#v, want 500 INTERNAL", rpcErr)
	}
	if strings.Contains(rpcErr.Message, secret) {
		t.Fatal("panic value was exposed in RPC output")
	}

	outcome, err = dispatcher.DispatchApplication(context.Background(), runtimeApplicationRequest(t, 109, "after"))
	if err != nil {
		t.Fatalf("dispatch after panic: %v", err)
	}
	result := requireRuntimeApplicationResult(t, outcome)
	response := &runtimeApplicationTestResponse{}
	if err := response.DeserializeTL(bytes.NewReader(result.Body)); err != nil {
		t.Fatalf("decode response after panic: %v", err)
	}
	if response.Value != "response:after" {
		t.Fatalf("response after panic = %q", response.Value)
	}
}

func TestRuntimeApplicationDispatcherRedactsUnknownHandlerError(t *testing.T) {
	const secret = "sentinel-handler-error-secret"
	server := NewServer()
	registerRuntimeApplicationTestService(server, &runtimeApplicationTestServiceImpl{
		call: func(context.Context, *runtimeApplicationTestRequest) (*runtimeApplicationTestResponse, error) {
			return nil, fmt.Errorf("database failed with password %s", secret)
		},
	})

	outcome, err := newRuntimeApplicationDispatcher(server).DispatchApplication(
		context.Background(), runtimeApplicationRequest(t, 110, "error"),
	)
	if err != nil {
		t.Fatalf("dispatch application: %v", err)
	}
	rpcErr := requireRuntimeApplicationError(t, outcome)
	if rpcErr.Code != int32(Internal) || rpcErr.Message != "INTERNAL" {
		t.Fatalf("unknown RPC error = %#v, want 500 INTERNAL", rpcErr)
	}
	if strings.Contains(rpcErr.Message, secret) {
		t.Fatal("unknown handler error was exposed in RPC output")
	}
}

func TestRuntimeApplicationDispatcherCollectsSuccessfulUserBindingMutation(t *testing.T) {
	server := NewServer()
	registerRuntimeApplicationTestService(server, &runtimeApplicationTestServiceImpl{
		call: func(ctx context.Context, _ *runtimeApplicationTestRequest) (*runtimeApplicationTestResponse, error) {
			if err := BindSessionUser(ctx, 701); err != nil {
				t.Fatal(err)
			}
			return &runtimeApplicationTestResponse{Value: "bound"}, nil
		},
	})
	outcome, err := newRuntimeApplicationDispatcher(server).DispatchApplication(
		context.Background(), runtimeApplicationRequest(t, 107, "bind"),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []runtimev2.SessionMutation{runtimev2.BindUser{UserID: 701}}
	if !reflect.DeepEqual(outcome.Mutations, want) {
		t.Fatalf("binding mutations = %#v, want %#v", outcome.Mutations, want)
	}
}

func TestRuntimeApplicationDispatcherRejectsTrailingRequestBytes(t *testing.T) {
	server := NewServer()
	called := false
	registerRuntimeApplicationTestService(server, &runtimeApplicationTestServiceImpl{
		call: func(context.Context, *runtimeApplicationTestRequest) (*runtimeApplicationTestResponse, error) {
			called = true
			return &runtimeApplicationTestResponse{}, nil
		},
	})
	request := runtimeApplicationRequest(t, 103, "trailing")
	request.Message.Body = append(request.Message.Body, 0xaa)

	outcome, err := newRuntimeApplicationDispatcher(server).DispatchApplication(context.Background(), request)
	if err != nil {
		t.Fatalf("dispatch application: %v", err)
	}
	rpcErr := requireRuntimeApplicationError(t, outcome)
	if rpcErr.Code != 400 || rpcErr.Message != "REQUEST_TRAILING_BYTES" {
		t.Fatalf("RPC error = %#v", rpcErr)
	}
	if called {
		t.Fatal("handler called for request with trailing bytes")
	}
}

func TestRuntimeApplicationDispatcherContextMetadataWithoutLegacyState(t *testing.T) {
	server := NewServer()
	registerRuntimeApplicationTestService(server, &runtimeApplicationTestServiceImpl{
		call: func(ctx context.Context, _ *runtimeApplicationTestRequest) (*runtimeApplicationTestResponse, error) {
			if got := AuthKeyIDFromContext(ctx); got != 91 {
				t.Fatalf("auth key ID = %d, want 91", got)
			}
			if got := UserIDFromContext(ctx); got != 93 {
				t.Fatalf("user ID = %d, want 93", got)
			}
			if got := LayerFromContext(ctx); got != 37 {
				t.Fatalf("layer = %d, want 37", got)
			}
			binding, ok := BindingFromContext(ctx)
			if !ok || binding.AuthKeyID != 91 || binding.SessionID != 92 || binding.UserID != 93 || binding.Layer != 37 {
				t.Fatalf("binding = %#v, present=%v", binding, ok)
			}
			return &runtimeApplicationTestResponse{Value: "metadata"}, nil
		},
	})

	outcome, err := newRuntimeApplicationDispatcher(server).DispatchApplication(
		context.Background(), runtimeApplicationRequest(t, 104, "metadata"),
	)
	if err != nil {
		t.Fatalf("dispatch application: %v", err)
	}
	requireRuntimeApplicationResult(t, outcome)
}

func TestRuntimeApplicationDispatcherInvokesInterceptor(t *testing.T) {
	interceptorCalls := 0
	server := NewServer(WithUnaryInterceptor(func(ctx context.Context, request interface{}, info *UnaryServerInfo, handler UnaryHandler) (interface{}, error) {
		interceptorCalls++
		if info.FullMethod != "/custom.SchemaService/Call" {
			t.Fatalf("full method = %q", info.FullMethod)
		}
		if _, ok := request.(*runtimeApplicationTestRequest); !ok {
			t.Fatalf("interceptor request type = %T", request)
		}
		return handler(ctx, request)
	}))
	registerRuntimeApplicationTestService(server, &runtimeApplicationTestServiceImpl{
		call: func(context.Context, *runtimeApplicationTestRequest) (*runtimeApplicationTestResponse, error) {
			return &runtimeApplicationTestResponse{Value: "intercepted"}, nil
		},
	})

	outcome, err := newRuntimeApplicationDispatcher(server).DispatchApplication(
		context.Background(), runtimeApplicationRequest(t, 105, "interceptor"),
	)
	if err != nil {
		t.Fatalf("dispatch application: %v", err)
	}
	requireRuntimeApplicationResult(t, outcome)
	if interceptorCalls != 1 {
		t.Fatalf("interceptor calls = %d, want 1", interceptorCalls)
	}
}

func TestRuntimeApplicationDispatcherMissingMethod(t *testing.T) {
	server := NewServer()
	request := runtimeApplicationRequest(t, 106, "missing")

	outcome, err := newRuntimeApplicationDispatcher(server).DispatchApplication(context.Background(), request)
	if err != nil {
		t.Fatalf("dispatch application: %v", err)
	}
	rpcErr := requireRuntimeApplicationError(t, outcome)
	if rpcErr.RequestMessageID != 106 || rpcErr.Code != 404 || rpcErr.Message != "METHOD_NOT_FOUND" {
		t.Fatalf("RPC error = %#v", rpcErr)
	}
}

func requireRuntimeApplicationResult(t *testing.T, outcome runtimev2.Outcome) runtimev2.RPCResult {
	t.Helper()
	if len(outcome.Intents) != 1 {
		t.Fatalf("intent count = %d, want 1", len(outcome.Intents))
	}
	result, ok := outcome.Intents[0].(runtimev2.RPCResult)
	if !ok {
		t.Fatalf("intent type = %T, want runtime.RPCResult", outcome.Intents[0])
	}
	return result
}

func requireRuntimeApplicationError(t *testing.T, outcome runtimev2.Outcome) runtimev2.RPCError {
	t.Helper()
	if len(outcome.Intents) != 1 {
		t.Fatalf("intent count = %d, want 1", len(outcome.Intents))
	}
	rpcErr, ok := outcome.Intents[0].(runtimev2.RPCError)
	if !ok {
		t.Fatalf("intent type = %T, want runtime.RPCError", outcome.Intents[0])
	}
	return rpcErr
}
