package compat

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/compat/client"
	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/examples/gen"
	"github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
)

func TestHandlerUserBindingIsPersistedBeforeReconnect(t *testing.T) {
	authKeys := crypto.NewMemoryAuthKeyManager()
	serverKeys := crypto.NewMemoryServerKeyManager()
	serverKey, err := crypto.GenerateServerKey()
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	serverKeys.AddKey(serverKey)
	sessions := &recordingSessionStore{Store: session.NewMemoryStore()}
	srv := tlrpc.NewServer(
		tlrpc.WithAuthKeyManager(authKeys),
		tlrpc.WithServerKeyManager(serverKeys),
		tlrpc.WithSessionStore(sessions),
	)

	seenUser := make(chan int64, 1)
	srv.RegisterService(tlrpc.ServiceDesc{
		ServiceName: "compat.binding.Session",
		SchemaLayer: gen.SchemaLayer,
		HandlerType: (*sessionBindingServer)(nil),
		Methods: []tlrpc.MethodDesc{
			{
				MethodName:    "SignIn",
				ConstructorID: (&gen.AuthSignInRequest{}).ConstructorID(),
				NewRequest:    func() tlrpc.TLObject { return &gen.AuthSignInRequest{} },
				Handler:       sessionBindingSignInHandler,
			},
			{
				MethodName:    "GetConfig",
				ConstructorID: (&gen.HelpGetConfigRequest{}).ConstructorID(),
				NewRequest:    func() tlrpc.TLObject { return &gen.HelpGetConfigRequest{} },
				Handler:       sessionBindingGetConfigHandler,
			},
		},
	}, &sessionBindingService{seenUser: seenUser})

	lis, err := (&transport.TCPTransport{Protocol: transport.ProtocolAbridged}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	runServer(t, srv, lis)
	cli := dialClientTCP(t, lis.Addr().String(), client.CodecAbridged, serverKey)
	if _, err := cli.Handshake(context.Background()); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if _, err := cli.Invoke(context.Background(), &gen.AuthSignInRequest{}); err != nil {
		t.Fatalf("sign in: %v", err)
	}
	info := cli.Session()
	canonicalAuthKeyID := info.AuthKey.ID()
	if info.AuthKeyID != canonicalAuthKeyID {
		t.Fatalf("handshake auth key ID = %d, canonical ID = %d", info.AuthKeyID, canonicalAuthKeyID)
	}
	if sessions.authorizedSaves.Load() == 0 {
		t.Fatal("authorized session was not saved after handler dispatch")
	}
	_ = cli.Close()

	reconnected := dialClientTCP(t, lis.Addr().String(), client.CodecAbridged, serverKey)
	defer func() { _ = reconnected.Close() }()
	reconnected.SetSessionInfo(info)
	if _, err := reconnected.Invoke(context.Background(), &gen.HelpGetConfigRequest{}); err != nil {
		t.Fatalf("invoke after reconnect: %v", err)
	}
	if got := <-seenUser; got != 77 {
		t.Fatalf("reconnected user = %d, want 77", got)
	}
}

type sessionBindingServer interface {
	SignIn(context.Context, *gen.AuthSignInRequest) (gen.AuthAuthorizationType, error)
	GetConfig(context.Context, *gen.HelpGetConfigRequest) (*gen.Config, error)
}

type sessionBindingService struct {
	seenUser chan<- int64
}

func (*sessionBindingService) SignIn(ctx context.Context, _ *gen.AuthSignInRequest) (gen.AuthAuthorizationType, error) {
	if err := tlrpc.BindSessionUser(ctx, 77); err != nil {
		return nil, err
	}
	return &gen.AuthAuthorization{User: &gen.UserEmpty{ID: 77}}, nil
}

func (s *sessionBindingService) GetConfig(ctx context.Context, _ *gen.HelpGetConfigRequest) (*gen.Config, error) {
	s.seenUser <- tlrpc.UserIDFromContext(ctx)
	return &gen.Config{ThisDc: 1}, nil
}

func sessionBindingSignInHandler(service interface{}, ctx context.Context, request *gen.AuthSignInRequest) (gen.AuthAuthorizationType, error) {
	return service.(sessionBindingServer).SignIn(ctx, request)
}

func sessionBindingGetConfigHandler(service interface{}, ctx context.Context, request *gen.HelpGetConfigRequest) (*gen.Config, error) {
	return service.(sessionBindingServer).GetConfig(ctx, request)
}

type recordingSessionStore struct {
	session.Store
	authorizedSaves atomic.Int64
}

func (s *recordingSessionStore) Save(ctx context.Context, key session.SessionKey, snapshot session.Snapshot) error {
	if snapshot.UserID != 0 {
		s.authorizedSaves.Add(1)
	}
	return s.Store.Save(ctx, key, snapshot)
}
