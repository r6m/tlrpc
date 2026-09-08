package compat

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/compat/client"
	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
)

const recoveryBarrierUserID int64 = 88001

type recoveryBarrierHarness struct {
	server *tlrpc.Server
	auth   crypto.AuthKeyManager
	store  *session.MemoryStore
	addr   string
	salt   int64
	ping   *pingService
	large  *largePayloadService
}

type recoveryBarrierIdentity struct {
	keyID   crypto.KeyID
	key     crypto.AuthKey
	session int64
}

func newRecoveryBarrierHarness(t *testing.T, extra ...tlrpc.ServerOption) *recoveryBarrierHarness {
	t.Helper()
	auth := crypto.NewMemoryAuthKeyManager()
	store := session.NewMemoryStore()
	options := []tlrpc.ServerOption{
		tlrpc.WithAuthKeyManager(auth),
		tlrpc.WithSessionStore(store),
		tlrpc.WithRecoveryPushBarrier(pingReqID),
	}
	options = append(options, extra...)
	server := tlrpc.NewServer(options...)
	ping := &pingService{}
	large := &largePayloadService{}
	registerPingService(server, ping)
	registerLargePayloadService(server, large)
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
		ping:   ping,
		large:  large,
	}
}

func (h *recoveryBarrierHarness) addIdentity(t *testing.T, seed byte, sessionID int64) recoveryBarrierIdentity {
	t.Helper()
	var key crypto.AuthKey
	for index := range key {
		key[index] = byte(index) ^ seed
	}
	keyID := key.ID()
	if err := h.auth.Put(keyID, key); err != nil {
		t.Fatalf("put auth key: %v", err)
	}
	if _, _, err := h.store.LoadOrCreate(context.Background(), session.SessionKey{
		AuthKeyID: keyID,
		SessionID: sessionID,
	}, session.Snapshot{
		AuthKeyID:  keyID,
		SessionID:  sessionID,
		ServerSalt: h.salt,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return recoveryBarrierIdentity{keyID: keyID, key: key, session: sessionID}
}

func (h *recoveryBarrierHarness) addSession(t *testing.T, identity recoveryBarrierIdentity, sessionID int64) recoveryBarrierIdentity {
	t.Helper()
	identity.session = sessionID
	if _, _, err := h.store.LoadOrCreate(context.Background(), session.SessionKey{
		AuthKeyID: identity.keyID,
		SessionID: sessionID,
	}, session.Snapshot{
		AuthKeyID:  identity.keyID,
		SessionID:  sessionID,
		ServerSalt: h.salt,
	}); err != nil {
		t.Fatalf("create sibling session: %v", err)
	}
	return identity
}

func (h *recoveryBarrierHarness) dial(t *testing.T, identity recoveryBarrierIdentity) *client.Client {
	t.Helper()
	conn, err := (&transport.TCPTransport{Protocol: transport.ProtocolIntermediate}).Dial(h.addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	cli := client.New(conn, client.WithConstructors(map[uint32]func() tlrpc.TLObject{
		pingRespID:          func() tlrpc.TLObject { return &pingResp{} },
		largePayloadRespID:  func() tlrpc.TLObject { return &largePayloadResp{} },
		runtimeWriterPushID: func() tlrpc.TLObject { return &runtimeWriterPush{} },
	}))
	cli.SetSession(identity.keyID, identity.key, h.salt, identity.session)
	return cli
}

func bindRecoveryBarrierSession(t *testing.T, cli *client.Client) {
	t.Helper()
	requestID := client.NextMsgID()
	writeEncryptedRequest(t, cli, requestID, 1, serializeCompatObject(t, &largePayloadReq{Size: 1}))
	drainRPCExchange(t, cli, requestID, true)
}

func readRecoveryBarrierInteresting(t *testing.T, cli *client.Client) (*mtproto.InnerData, tlrpc.TLObject) {
	t.Helper()
	for index := 0; index < 8; index++ {
		for _, decoded := range readRuntimeWriterWireObjects(t, cli) {
			if decoded.object.ConstructorID() == mtprototl.MsgsAckID {
				continue
			}
			return decoded.inner, decoded.object
		}
	}
	t.Fatal("interesting wire object not received")
	return nil, nil
}

func requireNoRecoveryBarrierWire(t *testing.T, cli *client.Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	object, err := cli.ReadOne(ctx)
	if err == nil {
		t.Fatalf("application push %T arrived while protected RPC active", object)
	}
	var networkError net.Error
	if !errors.Is(err, context.DeadlineExceeded) && (!errors.As(err, &networkError) || !networkError.Timeout()) {
		t.Fatalf("read application push while protected RPC active = %v, want deadline", err)
	}
}

func requireRecoveryBarrierRPC(t *testing.T, object tlrpc.TLObject, requestID int64, value int32) {
	t.Helper()
	result, ok := object.(*mtprototl.RPCResult)
	if !ok {
		t.Fatalf("object = %T, want rpc_result", object)
	}
	if result.ReqMsgID != requestID {
		t.Fatalf("rpc_result request = %d, want %d", result.ReqMsgID, requestID)
	}
	response := &pingResp{}
	if err := response.DeserializeTL(bytes.NewReader(result.ResultRaw)); err != nil {
		t.Fatalf("decode ping response: %v", err)
	}
	if response.Value != value {
		t.Fatalf("ping response = %d, want %d", response.Value, value)
	}
}

func requireRecoveryBarrierPush(t *testing.T, object tlrpc.TLObject, value int32) {
	t.Helper()
	push, ok := object.(*runtimeWriterPush)
	if !ok {
		t.Fatalf("object = %T, want runtimeWriterPush", object)
	}
	if push.Value != value {
		t.Fatalf("push = %d, want %d", push.Value, value)
	}
}

func TestRecoveryPushBarrierEncryptedReplyPrecedesQueuedPushFIFO(t *testing.T) {
	h := newRecoveryBarrierHarness(t)
	h.large.call = func(ctx context.Context, request *largePayloadReq) (*largePayloadResp, error) {
		if err := tlrpc.BindSessionUser(ctx, recoveryBarrierUserID); err != nil {
			return nil, err
		}
		return &largePayloadResp{}, nil
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	h.ping.call = func(ctx context.Context, request *pingReq) (*pingResp, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &pingResp{Value: request.Value + 1}, nil
	}
	identity := h.addIdentity(t, 0, 0x1010101020202020)
	cli := h.dial(t, identity)
	bindRecoveryBarrierSession(t, cli)

	requestID := client.NextMsgID()
	writeEncryptedRequest(t, cli, requestID, 3, serializeCompatObject(t, &pingReq{Value: 40}))
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("protected handler did not start")
	}
	for _, value := range []int32{91, 92} {
		published := make(chan error, 1)
		go func(value int32) {
			published <- h.server.Publish(recoveryBarrierUserID, &runtimeWriterPush{Value: value})
		}(value)
		select {
		case err := <-published:
			if err != nil {
				t.Fatalf("publish %d: %v", value, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("publish %d waited for RPC", value)
		}
	}
	requireNoRecoveryBarrierWire(t, cli)
	once.Do(func() { close(release) })

	_, object := readRecoveryBarrierInteresting(t, cli)
	requireRecoveryBarrierRPC(t, object, requestID, 41)
	for _, value := range []int32{91, 92} {
		_, object = readRecoveryBarrierInteresting(t, cli)
		requireRecoveryBarrierPush(t, object, value)
	}
}

func TestRecoveryPushBarrierIsExactSessionAndOrdinaryFileLikeRPCStillReceivesPush(t *testing.T) {
	h := newRecoveryBarrierHarness(t)
	ordinaryStarted := make(chan struct{})
	releaseOrdinary := make(chan struct{})
	h.large.call = func(ctx context.Context, request *largePayloadReq) (*largePayloadResp, error) {
		if err := tlrpc.BindSessionUser(ctx, recoveryBarrierUserID); err != nil {
			return nil, err
		}
		if request.Size == 2 {
			close(ordinaryStarted)
			select {
			case <-releaseOrdinary:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return &largePayloadResp{}, nil
	}
	protectedStarted := make(chan struct{})
	releaseProtected := make(chan struct{})
	h.ping.call = func(ctx context.Context, request *pingReq) (*pingResp, error) {
		close(protectedStarted)
		select {
		case <-releaseProtected:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &pingResp{Value: request.Value + 1}, nil
	}
	var cleanup sync.Once
	t.Cleanup(func() {
		cleanup.Do(func() {
			close(releaseOrdinary)
			close(releaseProtected)
		})
	})

	mainIdentity := h.addIdentity(t, 0, 0x2020202030303030)
	fileIdentity := h.addSession(t, mainIdentity, 0x2020202030303031)
	otherAuthIdentity := h.addIdentity(t, 0x55, 0x3030303040404040)
	mainClient := h.dial(t, mainIdentity)
	fileClient := h.dial(t, fileIdentity)
	otherAuthClient := h.dial(t, otherAuthIdentity)
	for _, cli := range []*client.Client{mainClient, fileClient, otherAuthClient} {
		bindRecoveryBarrierSession(t, cli)
	}

	protectedRequestID := client.NextMsgID()
	writeEncryptedRequest(t, mainClient, protectedRequestID, 3, serializeCompatObject(t, &pingReq{Value: 10}))
	ordinaryRequestID := client.NextMsgID()
	writeEncryptedRequest(t, fileClient, ordinaryRequestID, 3, serializeCompatObject(t, &largePayloadReq{Size: 2}))
	for name, started := range map[string]<-chan struct{}{"protected": protectedStarted, "ordinary": ordinaryStarted} {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s handler did not start", name)
		}
	}
	if err := h.server.Publish(recoveryBarrierUserID, &runtimeWriterPush{Value: 77}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	for name, cli := range map[string]*client.Client{"same-auth ordinary file-like session": fileClient, "different-auth session": otherAuthClient} {
		_, object := readRecoveryBarrierInteresting(t, cli)
		if object.ConstructorID() != runtimeWriterPushID {
			t.Fatalf("%s first object = %T, want immediate push", name, object)
		}
		requireRecoveryBarrierPush(t, object, 77)
	}
	requireNoRecoveryBarrierWire(t, mainClient)
	cleanup.Do(func() {
		close(releaseOrdinary)
		close(releaseProtected)
	})
	_, object := readRecoveryBarrierInteresting(t, mainClient)
	requireRecoveryBarrierRPC(t, object, protectedRequestID, 11)
	_, object = readRecoveryBarrierInteresting(t, mainClient)
	requireRecoveryBarrierPush(t, object, 77)
}

func TestRecoveryPushBarrierWaitsForAllProtectedRPCReplies(t *testing.T) {
	h := newRecoveryBarrierHarness(t)
	h.large.call = func(ctx context.Context, request *largePayloadReq) (*largePayloadResp, error) {
		if err := tlrpc.BindSessionUser(ctx, recoveryBarrierUserID); err != nil {
			return nil, err
		}
		return &largePayloadResp{}, nil
	}
	started := make(chan int32, 2)
	releases := map[int32]chan struct{}{1: make(chan struct{}), 2: make(chan struct{})}
	h.ping.call = func(ctx context.Context, request *pingReq) (*pingResp, error) {
		started <- request.Value
		select {
		case <-releases[request.Value]:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &pingResp{Value: request.Value + 10}, nil
	}
	t.Cleanup(func() {
		for _, release := range releases {
			select {
			case <-release:
			default:
				close(release)
			}
		}
	})
	identity := h.addIdentity(t, 0, 0x4040404050505050)
	cli := h.dial(t, identity)
	bindRecoveryBarrierSession(t, cli)
	firstRequestID := client.NextMsgID()
	requestIDs := []int64{firstRequestID, firstRequestID + 4}
	writeEncryptedRequest(t, cli, requestIDs[0], 3, serializeCompatObject(t, &pingReq{Value: 1}))
	writeEncryptedRequest(t, cli, requestIDs[1], 5, serializeCompatObject(t, &pingReq{Value: 2}))
	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("protected handlers did not both start")
		}
	}
	if err := h.server.Publish(recoveryBarrierUserID, &runtimeWriterPush{Value: 88}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	close(releases[1])
	_, object := readRecoveryBarrierInteresting(t, cli)
	requireRecoveryBarrierRPC(t, object, requestIDs[0], 11)
	requireNoRecoveryBarrierWire(t, cli)
	close(releases[2])
	_, object = readRecoveryBarrierInteresting(t, cli)
	requireRecoveryBarrierRPC(t, object, requestIDs[1], 12)
	_, object = readRecoveryBarrierInteresting(t, cli)
	requireRecoveryBarrierPush(t, object, 88)
}

func TestRecoveryPushBarrierRPCErrorPrecedesQueuedPush(t *testing.T) {
	h := newRecoveryBarrierHarness(t)
	h.large.call = func(ctx context.Context, request *largePayloadReq) (*largePayloadResp, error) {
		if err := tlrpc.BindSessionUser(ctx, recoveryBarrierUserID); err != nil {
			return nil, err
		}
		return &largePayloadResp{}, nil
	}
	started := make(chan struct{})
	release := make(chan struct{})
	h.ping.call = func(ctx context.Context, request *pingReq) (*pingResp, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return nil, tlrpc.NewRPCError(400, "RECOVERY_TEST")
	}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	identity := h.addIdentity(t, 0, 0x5050505060606060)
	cli := h.dial(t, identity)
	bindRecoveryBarrierSession(t, cli)
	requestID := client.NextMsgID()
	writeEncryptedRequest(t, cli, requestID, 3, serializeCompatObject(t, &pingReq{Value: 1}))
	<-started
	if err := h.server.Publish(recoveryBarrierUserID, &runtimeWriterPush{Value: 66}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	close(release)
	_, object := readRecoveryBarrierInteresting(t, cli)
	result, ok := object.(*mtprototl.RPCResult)
	if !ok || result.ReqMsgID != requestID {
		t.Fatalf("first object = %#v, want correlated rpc_result", object)
	}
	rpcError := &mtprototl.RPCError{}
	if err := rpcError.DeserializeTL(bytes.NewReader(result.ResultRaw)); err != nil {
		t.Fatalf("decode rpc_error: %v", err)
	}
	if rpcError.ErrorCode != 400 || rpcError.ErrorMessage != "RECOVERY_TEST" {
		t.Fatalf("rpc_error = %d %q", rpcError.ErrorCode, rpcError.ErrorMessage)
	}
	_, object = readRecoveryBarrierInteresting(t, cli)
	requireRecoveryBarrierPush(t, object, 66)
}

func TestRecoveryPushBarrierOverflowReturnsExplicitError(t *testing.T) {
	h := newRecoveryBarrierHarness(t, tlrpc.WithResourceLimits(tlrpc.ResourceLimits{
		MaxPayloadBytes:            1 << 20,
		MaxInFlightRequests:        8,
		MaxEncodedResponseBytes:    1 << 10,
		PhysicalWriteQueueCapacity: 1,
		ReadTimeout:                5 * time.Second,
		WriteTimeout:               5 * time.Second,
	}))
	h.large.call = func(ctx context.Context, request *largePayloadReq) (*largePayloadResp, error) {
		if err := tlrpc.BindSessionUser(ctx, recoveryBarrierUserID); err != nil {
			return nil, err
		}
		return &largePayloadResp{}, nil
	}
	started := make(chan struct{})
	release := make(chan struct{})
	h.ping.call = func(ctx context.Context, request *pingReq) (*pingResp, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &pingResp{Value: 1}, nil
	}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	identity := h.addIdentity(t, 0, 0x6060606070707070)
	cli := h.dial(t, identity)
	bindRecoveryBarrierSession(t, cli)
	writeEncryptedRequest(t, cli, client.NextMsgID(), 3, serializeCompatObject(t, &pingReq{Value: 1}))
	<-started
	if err := h.server.Publish(recoveryBarrierUserID, &runtimeWriterPush{Value: 1}); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	err := h.server.Publish(recoveryBarrierUserID, &runtimeWriterPush{Value: 2})
	if !errors.Is(err, tlrpc.ErrRecoveryPushBarrierFull) {
		t.Fatalf("second publish error = %v, want ErrRecoveryPushBarrierFull", err)
	}
}
