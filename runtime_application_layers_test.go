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
		{MethodName: "Call", ConstructorID: runtimeApplicationRequestID, MinLayer: 228, MaxLayer: 228, NewRequest: func() TLObject { return &runtimeApplicationTestRequest{} }, Handler: runtimeApplicationTestHandler},
		{MethodName: "CallLayer229", ConstructorID: runtimeApplicationRequestID, MinLayer: 229, MaxLayer: 229, NewRequest: func() TLObject { return &layer229ApplicationRequest{} }, Handler: func(s interface{}, ctx context.Context, r *layer229ApplicationRequest) (*runtimeApplicationTestResponse, error) {
			return s.(runtimeApplicationTestService).Call(ctx, &runtimeApplicationTestRequest{Value: fmt.Sprintf("%s:%d", r.Value, r.Extra)})
		}},
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
