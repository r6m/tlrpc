package harness

import (
	"context"
	"fmt"
	"time"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/compat/client"
	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/examples/gen"
	"github.com/r6m/tlrpc/internal/compatkeys"
	"github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
)

// Server wraps a minimal TLRPC server for compat tests.
type Server struct {
	Server   *tlrpc.Server
	TCP      transport.Listener
	TCPAddr  string
	AuthKeys crypto.AuthKeyManager
	Sessions session.Store
}

// Start creates a minimal compat server with in-memory state and a single TCP transport.
func Start() (*Server, error) {
	compatKey, err := compatkeys.ServerKey()
	if err != nil {
		return nil, fmt.Errorf("load compat key: %w", err)
	}
	serverKeys := crypto.NewMemoryServerKeyManager()
	serverKeys.AddKey(compatKey)

	authKeys := crypto.NewMemoryAuthKeyManager()
	sessions := session.NewMemoryStore()

	srv := tlrpc.NewServer(
		tlrpc.WithAuthKeyManager(authKeys),
		tlrpc.WithSessionStore(sessions),
		tlrpc.WithServerKeyManager(serverKeys),
	)

	gen.RegisterHelpServer(srv, &helpService{})
	gen.RegisterAuthServer(srv, &authService{})

	tcpLis, err := (&transport.TCPTransport{AllowObfuscation: true}).Listen("127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen tcp: %w", err)
	}
	go func() {
		_ = srv.ServeTransport(tcpLis)
	}()

	return &Server{
		Server:   srv,
		TCP:      tcpLis,
		TCPAddr:  tcpLis.Addr().String(),
		AuthKeys: authKeys,
		Sessions: sessions,
	}, nil
}

// Close stops the server and closes listeners.
func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	if s.Server != nil {
		_ = s.Server.Stop()
	}
	if s.TCP != nil {
		_ = s.TCP.Close()
	}
	return nil
}

// DialTCP creates a compat client with schema constructors registered.
func DialTCP(addr string, extraConstructors map[uint32]func() tlrpc.TLObject) (*client.Client, error) {
	compatKey, err := compatkeys.ServerKey()
	if err != nil {
		return nil, fmt.Errorf("load compat key: %w", err)
	}
	constructors := gen.GetStaticConstructors()
	for id, ctor := range extraConstructors {
		constructors[id] = ctor
	}
	cli, err := client.DialTCP(addr, client.CodecAbridged,
		client.WithServerKey(compatKey),
		client.WithConstructors(constructors),
	)
	if err != nil {
		return nil, err
	}
	return cli, nil
}

func defaultInitParams() client.InitParams {
	return client.InitParams{
		APIID:          77777,
		DeviceModel:    "compat-harness",
		SystemVersion:  "test",
		AppVersion:     "1.0",
		SystemLangCode: "en",
		LangPack:       "",
		LangCode:       "en",
	}
}

type helpService struct{ gen.UnimplementedHelpServer }

func (s *helpService) GetConfig(context.Context, *gen.HelpGetConfigRequest) (*gen.Config, error) {
	now := int32(time.Now().Unix())
	return &gen.Config{
		Date:             now,
		Expires:          now + 3600,
		ThisDc:           1,
		DcTxtDomainName:  "localhost",
		ChatSizeMax:      200,
		MessageLengthMax: 4096,
	}, nil
}

func (s *helpService) GetNearestDc(context.Context, *gen.HelpGetNearestDcRequest) (*gen.NearestDc, error) {
	return nil, tlrpc.NewBadRequestError("NEAREST_DC_UNAVAILABLE")
}

type authService struct{ gen.UnimplementedAuthServer }

func (s *authService) SignIn(ctx context.Context, _ *gen.AuthSignInRequest) (gen.AuthAuthorizationType, error) {
	const userID int64 = 1
	if err := tlrpc.BindSessionUser(ctx, userID); err != nil {
		return nil, err
	}
	return &gen.AuthAuthorization{User: &gen.UserEmpty{ID: userID}}, nil
}
