package tlrpc

import (
	"bytes"
	"context"
	"fmt"
	"github.com/r6m/tlrpc/mtproto"
	"io"
	"testing"
)

type layer229ApplicationRequest struct {
	runtimeApplicationTestRequest
	Extra int32
}

func (r *layer229ApplicationRequest) DeserializeTL(rd io.Reader) error {
	if err := r.runtimeApplicationTestRequest.DeserializeTL(rd); err != nil {
		return err
	}
	value, err := mtproto.ReadInt32(rd)
	r.Extra = value
	return err
}

func TestRuntimeApplicationSelectsSameConstructorBySessionLayer(t *testing.T) {
	server := NewServer()
	impl := &runtimeApplicationTestServiceImpl{call: func(ctx context.Context, req *runtimeApplicationTestRequest) (*runtimeApplicationTestResponse, error) {
		return &runtimeApplicationTestResponse{Value: fmt.Sprintf("%d:%s", LayerFromContext(ctx), req.Value)}, nil
	}}
	server.RegisterService(ServiceDesc{ServiceName: "layered", SchemaLayer: 229, HandlerType: (*runtimeApplicationTestService)(nil), Methods: []MethodDesc{
		{MethodName: "Call", ConstructorID: runtimeApplicationRequestID, MinLayer: 228, MaxLayer: 228, NewRequest: func() TLObject { return &runtimeApplicationTestRequest{} }, Handler: runtimeApplicationMethodHandler, EncodeResponse: encodeRuntimeApplicationTestResponse},
		{MethodName: "CallLayer229", ConstructorID: runtimeApplicationRequestID, MinLayer: 229, MaxLayer: 229, NewRequest: func() TLObject { return &layer229ApplicationRequest{} }, EncodeResponse: encodeRuntimeApplicationTestResponse, Handler: BindMethod(func(s interface{}, ctx context.Context, r *layer229ApplicationRequest) (*runtimeApplicationTestResponse, error) {
			return s.(runtimeApplicationTestService).Call(ctx, &runtimeApplicationTestRequest{Value: fmt.Sprintf("%s:%d", r.Value, r.Extra)})
		})},
	}}, impl)
	dispatcher := newRuntimeApplicationDispatcher(server)
	for _, layer := range []int{228, 229, 228, 229} {
		req := runtimeApplicationRequest(t, 101, "value")
		req.Info.Layer = layer
		want := "228:value"
		if layer == 229 {
			var extra bytes.Buffer
			if err := mtproto.WriteInt32(&extra, 17); err != nil {
				t.Fatal(err)
			}
			req.Message.Body = append(req.Message.Body, extra.Bytes()...)
			want = "229:value:17"
		}
		outcome, err := dispatcher.DispatchApplication(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		result := requireRuntimeApplicationResult(t, outcome)
		response := &runtimeApplicationTestResponse{}
		if err := response.DeserializeTL(bytes.NewReader(result.Body)); err != nil {
			t.Fatal(err)
		}
		if response.Value != want {
			t.Fatalf("got %q want %q", response.Value, want)
		}
	}
}

type countedLayerRequest struct {
	runtimeApplicationTestRequest
	decoded *int
}

func (r *countedLayerRequest) DeserializeTL(rd io.Reader) error {
	*r.decoded++
	return r.runtimeApplicationTestRequest.DeserializeTL(rd)
}

func TestRuntimeApplicationRejectsExpiredMethodBeforeDecoding(t *testing.T) {
	decoded, intercepted, handled := 0, 0, 0
	server := NewServer(WithUnaryInterceptor(func(ctx context.Context, req any, info *UnaryServerInfo, next UnaryHandler) (any, error) {
		intercepted++
		return next(ctx, req)
	}))
	descriptor := MethodDesc{
		MethodName: "Call", ConstructorID: runtimeApplicationRequestID,
		MinLayer: 228, MaxLayer: 228,
		NewRequest: func() TLObject { return &countedLayerRequest{decoded: &decoded} },
		Handler: BindMethod(func(_ any, _ context.Context, _ *countedLayerRequest) (*runtimeApplicationTestResponse, error) {
			handled++
			return &runtimeApplicationTestResponse{Value: "accepted"}, nil
		}),
		EncodeResponse: encodeRuntimeApplicationTestResponse,
	}
	server.RegisterService(ServiceDesc{ServiceName: "strict", SchemaLayer: 230, HandlerType: (*runtimeApplicationTestService)(nil), Methods: []MethodDesc{descriptor}}, &runtimeApplicationTestServiceImpl{})
	dispatcher := newRuntimeApplicationDispatcher(server)
	for _, layer := range []int{227, 229, 230} {
		req := runtimeApplicationRequest(t, 101, "expired")
		req.Info.Layer = layer
		outcome, err := dispatcher.DispatchApplication(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		rpcErr := requireRuntimeApplicationError(t, outcome)
		if rpcErr.Code != 404 || rpcErr.Message != "METHOD_NOT_FOUND" || rpcErr.RequestMessageID != 101 {
			t.Fatalf("layer %d: %+v", layer, rpcErr)
		}
	}
	if decoded != 0 || intercepted != 0 || handled != 0 {
		t.Fatalf("expired call reached decode/interceptor/handler: %d/%d/%d", decoded, intercepted, handled)
	}
	req := runtimeApplicationRequest(t, 103, "valid")
	req.Info.Layer = 228
	outcome, err := dispatcher.DispatchApplication(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	requireRuntimeApplicationResult(t, outcome)
	if decoded != 1 || intercepted != 1 || handled != 1 {
		t.Fatalf("valid call did not run once: %d/%d/%d", decoded, intercepted, handled)
	}
}

func TestRuntimeApplicationReintroducedMethodKeepsAvailabilityGap(t *testing.T) {
	method := MethodDesc{MethodName: "Call", ConstructorID: runtimeApplicationRequestID,
		MinLayer: 228, MaxLayer: 228,
		NewRequest:     func() TLObject { return &runtimeApplicationTestRequest{} },
		Handler:        runtimeApplicationMethodHandler,
		EncodeResponse: encodeRuntimeApplicationTestResponse,
	}
	reintroduced := method
	reintroduced.MinLayer, reintroduced.MaxLayer = 230, 230
	server := NewServer()
	server.RegisterService(ServiceDesc{ServiceName: "recurring", SchemaLayer: 230, HandlerType: (*runtimeApplicationTestService)(nil), Methods: []MethodDesc{method, reintroduced}}, &runtimeApplicationTestServiceImpl{call: func(context.Context, *runtimeApplicationTestRequest) (*runtimeApplicationTestResponse, error) {
		return &runtimeApplicationTestResponse{Value: "ok"}, nil
	}})
	dispatcher := newRuntimeApplicationDispatcher(server)
	for _, layer := range []int{228, 229, 230} {
		req := runtimeApplicationRequest(t, 101, "request")
		req.Info.Layer = layer
		outcome, err := dispatcher.DispatchApplication(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if layer == 229 {
			if rpcErr := requireRuntimeApplicationError(t, outcome); rpcErr.Message != "METHOD_NOT_FOUND" {
				t.Fatalf("gap error: %+v", rpcErr)
			}
		} else {
			requireRuntimeApplicationResult(t, outcome)
		}
	}
}
