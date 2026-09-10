package compat

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/compat/client"
	"github.com/r6m/tlrpc/crypto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
)

type mixedLayerProjectionService interface {
	LargePayload(context.Context, *largePayloadReq) (*largePayloadResp, error)
}

type mixedLayerProjectionServiceImpl struct {
	call func(context.Context, *largePayloadReq) (*largePayloadResp, error)
}

func (s *mixedLayerProjectionServiceImpl) LargePayload(ctx context.Context, request *largePayloadReq) (*largePayloadResp, error) {
	return s.call(ctx, request)
}

func newMixedLayerProjectionHarness(t *testing.T, service mixedLayerProjectionService) *recoveryBarrierHarness {
	t.Helper()
	auth := crypto.NewMemoryAuthKeyManager()
	store := session.NewMemoryStore()
	server := tlrpc.NewServer(
		tlrpc.WithAuthKeyManager(auth),
		tlrpc.WithSessionStore(store),
		tlrpc.WithRecoveryPushBarrier(largePayloadReqID),
	)
	server.RegisterService(tlrpc.ServiceDesc{
		ServiceName: "compat.MixedLayerProjectionService",
		SchemaLayer: 229,
		HandlerType: (*mixedLayerProjectionService)(nil),
		Methods: []tlrpc.MethodDesc{{
			MethodName: "LargePayload", ConstructorID: largePayloadReqID,
			NewRequest: func() tlrpc.TLObject { return &largePayloadReq{} },
			Handler:    tlrpc.BindMethod(largePayloadServiceHandler), EncodeResponse: encodeCompatResponse[*largePayloadResp],
		}},
	}, service)
	lis, err := (&transport.TCPTransport{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	runServer(t, server, lis)
	return &recoveryBarrierHarness{
		server: server,
		auth:   auth,
		store:  store,
		addr:   lis.Addr().String(),
		salt:   0x1122334455667788,
	}
}

func TestProjectedPushEncryptedMixedLayersPreservesBarrierFIFOAndLeaseIdentity(t *testing.T) {
	const protectedSize int32 = 900
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

	service := &mixedLayerProjectionServiceImpl{}
	service.call = func(ctx context.Context, request *largePayloadReq) (*largePayloadResp, error) {
		if err := tlrpc.BindSessionUser(ctx, recoveryBarrierUserID); err != nil {
			return nil, err
		}
		if request.Size == protectedSize {
			binding, ok := tlrpc.BindingFromContext(ctx)
			if !ok || binding.Layer != 228 {
				t.Fatalf("protected request binding = %#v, present=%v", binding, ok)
			}
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return &largePayloadResp{}, nil
	}
	h := newMixedLayerProjectionHarness(t, service)
	identity228 := h.addIdentity(t, 0x28, 0x2280228022802280)
	identity229 := h.addIdentity(t, 0x29, 0x2290229022902290)
	client228 := h.dial(t, identity228)
	client229 := h.dial(t, identity229)

	for _, target := range []struct {
		client *client.Client
		layer  int32
	}{
		{client: client228, layer: 228},
		{client: client229, layer: 229},
	} {
		requestID := client.NextMsgID()
		query := serializeCompatObject(t, &largePayloadReq{Size: target.layer})
		body := serializeCompatObject(t, &mtprototl.InvokeWithLayer{Layer: target.layer, QueryRaw: query})
		writeEncryptedRequest(t, target.client, requestID, 1, body)
		drainRPCExchange(t, target.client, requestID, true)
	}

	protectedRequestID := client.NextMsgID()
	writeEncryptedRequest(t, client228, protectedRequestID, 3, serializeCompatObject(t, &largePayloadReq{Size: protectedSize}))
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("protected layer-228 handler did not start")
	}

	bindings := make(map[int64]tlrpc.Binding)
	projected := make(map[int]*runtimeWriterPush)
	if err := h.server.PublishProjected(recoveryBarrierUserID, func(_ context.Context, binding tlrpc.Binding) (tlrpc.TLObject, error) {
		bindings[binding.SessionID] = binding
		update := &runtimeWriterPush{Value: int32(binding.Layer)}
		projected[binding.Layer] = update
		return update, nil
	}); err != nil {
		t.Fatalf("publish projected: %v", err)
	}
	if len(bindings) != 2 || bindings[identity228.session].Layer != 228 || bindings[identity229.session].Layer != 229 {
		t.Fatalf("projected bindings = %#v", bindings)
	}
	if bindings[identity228.session].LeaseGeneration <= 0 || bindings[identity229.session].LeaseGeneration <= 0 {
		t.Fatalf("projected lease generations = (%d, %d), want positive", bindings[identity228.session].LeaseGeneration, bindings[identity229.session].LeaseGeneration)
	}
	projected[228].Value = -228
	projected[229].Value = -229

	_, object := readRecoveryBarrierInteresting(t, client229)
	requireRecoveryBarrierPush(t, object, 229)
	requireNoRecoveryBarrierWire(t, client228)
	releaseOnce.Do(func() { close(release) })
	_, object = readRecoveryBarrierInteresting(t, client228)
	result, ok := object.(*mtprototl.RPCResult)
	if !ok || result.ReqMsgID != protectedRequestID {
		t.Fatalf("protected response = %#v, want rpc_result for %d", object, protectedRequestID)
	}
	_, object = readRecoveryBarrierInteresting(t, client228)
	requireRecoveryBarrierPush(t, object, 228)
}
