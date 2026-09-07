package compat

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/compat/client"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
)

func nonSubscribingInitParams() client.InitParams {
	return client.InitParams{
		APIID:          1000,
		DeviceModel:    "compat",
		SystemVersion:  "test",
		AppVersion:     "1.0",
		SystemLangCode: "en",
		LangCode:       "en",
	}
}

func invokeNonSubscribingTest(t *testing.T, cli *client.Client, request tlrpc.TLObject) tlrpc.TLObject {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, err := cli.Invoke(ctx, request)
	if err != nil {
		t.Fatalf("invoke %T: %v", request, err)
	}
	return response
}

func invokeWrappedNonSubscribingTest(t *testing.T, cli *client.Client, request tlrpc.TLObject, withoutUpdates bool) tlrpc.TLObject {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, err := cli.InvokeWrapped(ctx, 170, nonSubscribingInitParams(), request, withoutUpdates)
	if err != nil {
		t.Fatalf("invoke wrapped %T withoutUpdates=%t: %v", request, withoutUpdates, err)
	}
	return response
}

func configureNonSubscribingBindHandlers(h *recoveryBarrierHarness) {
	h.large.call = func(ctx context.Context, request *largePayloadReq) (*largePayloadResp, error) {
		if err := tlrpc.BindSessionUser(ctx, recoveryBarrierUserID); err != nil {
			return nil, err
		}
		return &largePayloadResp{}, nil
	}
	h.ping.call = func(ctx context.Context, request *pingReq) (*pingResp, error) {
		if err := tlrpc.BindSessionUser(ctx, recoveryBarrierUserID); err != nil {
			return nil, err
		}
		return &pingResp{Value: request.Value + 1}, nil
	}
}

func TestNonSubscribingMethodKeepsColdSiblingExcludedWhilePrimaryReceivesPush(t *testing.T) {
	h := newRecoveryBarrierHarness(t, tlrpc.WithNonSubscribingMethods(largePayloadReqID))
	configureNonSubscribingBindHandlers(h)
	primaryIdentity := h.addIdentity(t, 0, 0x7171717121212121)
	fileIdentity := h.addSession(t, primaryIdentity, 0x7171717121212122)
	primary := h.dial(t, primaryIdentity)
	file := h.dial(t, fileIdentity)

	invokeNonSubscribingTest(t, file, &largePayloadReq{Size: 1})
	invokeNonSubscribingTest(t, primary, &pingReq{Value: 10})
	if err := h.server.Publish(recoveryBarrierUserID, &runtimeWriterPush{Value: 71}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	object, err := primary.ReadOne(ctx)
	if err != nil {
		t.Fatalf("read primary push: %v", err)
	}
	requireRecoveryBarrierPush(t, object, 71)
	requireNoRecoveryBarrierWire(t, file)
}

func TestNonSubscribingMethodPreservesExistingPrimarySubscription(t *testing.T) {
	h := newRecoveryBarrierHarness(t, tlrpc.WithNonSubscribingMethods(largePayloadReqID))
	configureNonSubscribingBindHandlers(h)
	identity := h.addIdentity(t, 0, 0x7272727222222222)
	primary := h.dial(t, identity)

	invokeNonSubscribingTest(t, primary, &pingReq{Value: 1})
	invokeNonSubscribingTest(t, primary, &largePayloadReq{Size: 1})
	if err := h.server.Publish(recoveryBarrierUserID, &runtimeWriterPush{Value: 72}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	object, err := primary.ReadOne(ctx)
	if err != nil {
		t.Fatalf("read preserved primary push: %v", err)
	}
	requireRecoveryBarrierPush(t, object, 72)
}

func TestNonSubscribingMethodMatchesInvokeWithoutUpdatesAfterWrapperNormalization(t *testing.T) {
	h := newRecoveryBarrierHarness(t, tlrpc.WithNonSubscribingMethods(largePayloadReqID))
	configureNonSubscribingBindHandlers(h)
	baseIdentity := h.addIdentity(t, 0, 0x7373737323232323)
	policyIdentity := h.addSession(t, baseIdentity, 0x7373737323232324)
	wrapperIdentity := h.addSession(t, baseIdentity, 0x7373737323232325)
	policyClient := h.dial(t, policyIdentity)
	wrapperClient := h.dial(t, wrapperIdentity)

	invokeWrappedNonSubscribingTest(t, policyClient, &largePayloadReq{Size: 1}, false)
	invokeWrappedNonSubscribingTest(t, wrapperClient, &pingReq{Value: 1}, true)
	if err := h.server.Publish(recoveryBarrierUserID, &runtimeWriterPush{Value: 73}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	requireNoRecoveryBarrierWire(t, policyClient)
	requireNoRecoveryBarrierWire(t, wrapperClient)
}

func TestRecoveryBarrierPushCannotBypassThroughNonSubscribingSibling(t *testing.T) {
	h := newRecoveryBarrierHarness(t, tlrpc.WithNonSubscribingMethods(largePayloadReqID))
	configureNonSubscribingBindHandlers(h)
	primaryIdentity := h.addIdentity(t, 0, 0x7474747424242424)
	fileIdentity := h.addSession(t, primaryIdentity, 0x7474747424242425)
	primary := h.dial(t, primaryIdentity)
	file := h.dial(t, fileIdentity)

	invokeNonSubscribingTest(t, file, &largePayloadReq{Size: 1})
	invokeNonSubscribingTest(t, primary, &pingReq{Value: 1})

	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	h.ping.call = func(ctx context.Context, request *pingReq) (*pingResp, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &pingResp{Value: request.Value + 1}, nil
	}
	requestID := client.NextMsgID()
	writeEncryptedRequest(t, primary, requestID, 3, serializeCompatObject(t, &pingReq{Value: 20}))
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("protected recovery handler did not start")
	}
	if err := h.server.Publish(recoveryBarrierUserID, &runtimeWriterPush{Value: 74}); err != nil {
		t.Fatalf("publish during recovery: %v", err)
	}
	requireNoRecoveryBarrierWire(t, primary)
	requireNoRecoveryBarrierWire(t, file)
	releaseOnce.Do(func() { close(release) })

	_, object := readRecoveryBarrierInteresting(t, primary)
	result, ok := object.(*mtprototl.RPCResult)
	if !ok || result.ReqMsgID != requestID {
		t.Fatalf("first primary object = %#v, want protected rpc_result", object)
	}
	_, object = readRecoveryBarrierInteresting(t, primary)
	requireRecoveryBarrierPush(t, object, 74)
	requireNoRecoveryBarrierWire(t, file)
}
