package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/examples/gen"
	"github.com/r6m/tlrpc/internal/compatkeys"
	"github.com/r6m/tlrpc/transport"
)

type stdLogger struct{}

func (stdLogger) Info(msg string, args ...interface{})  { log.Printf("INFO %s %v", msg, args) }
func (stdLogger) Error(msg string, args ...interface{}) { log.Printf("ERROR %s %v", msg, args) }
func (stdLogger) Debug(msg string, args ...interface{}) { log.Printf("DEBUG %s %v", msg, args) }

type helpService struct {
	gen.UnimplementedHelpServer
	dcIP    string
	tcpPort int32
}

func (s *helpService) GetConfig(context.Context, *gen.HelpGetConfigRequest) (*gen.Config, error) {
	now := int32(time.Now().Unix())
	return &gen.Config{
		Date:                    now,
		Expires:                 now + 86400,
		ThisDc:                  1,
		DcOptions:               []gen.DcOption{{ID: 1, IPAddress: s.dcIP, Port: s.tcpPort}},
		DcTxtDomainName:         "localhost",
		ChatSizeMax:             200,
		MegagroupSizeMax:        200000,
		ForwardedCountMax:       100,
		OnlineUpdatePeriodMs:    30000,
		OfflineBlurTimeoutMs:    5000,
		OfflineIdleTimeoutMs:    30000,
		OnlineCloudTimeoutMs:    30000,
		NotifyCloudDelayMs:      30000,
		NotifyDefaultDelayMs:    1500,
		PushChatPeriodMs:        60000,
		PushChatLimit:           2,
		EditTimeLimit:           172800,
		RevokeTimeLimit:         172800,
		RevokePmTimeLimit:       172800,
		RatingEDecay:            2419200,
		StickersRecentLimit:     30,
		ChannelsReadMediaPeriod: 604800,
		CallReceiveTimeoutMs:    20000,
		CallRingTimeoutMs:       90000,
		CallConnectTimeoutMs:    30000,
		CallPacketTimeoutMs:     10000,
		MeURLPrefix:             "https://t.me/",
		CaptionLengthMax:        1024,
		MessageLengthMax:        4096,
		WebfileDcID:             1,
	}, nil
}

func (s *helpService) GetNearestDc(context.Context, *gen.HelpGetNearestDcRequest) (*gen.NearestDc, error) {
	return &gen.NearestDc{
		Country:   "US",
		ThisDc:    1,
		NearestDc: 1,
	}, nil
}

func (s *helpService) GetAppConfig(context.Context, *gen.HelpGetAppConfigRequest) (*gen.HelpAppConfigType, error) {
	cfg := gen.HelpAppConfigType(&gen.HelpAppConfig{
		Hash:   0,
		Config: &gen.JSONNull{},
	})
	return &cfg, nil
}

type updatesService struct {
	gen.UnimplementedUpdatesServer
}

func (s *updatesService) GetState(context.Context, *gen.UpdatesGetStateRequest) (*gen.UpdatesState, error) {
	return &gen.UpdatesState{
		Pts:         1,
		Qts:         0,
		Date:        int32(time.Now().Unix()),
		Seq:         1,
		UnreadCount: 0,
	}, nil
}

func (s *updatesService) GetDifference(context.Context, *gen.UpdatesGetDifferenceRequest) (*gen.UpdatesDifferenceType, error) {
	diff := gen.UpdatesDifferenceType(&gen.UpdatesDifferenceEmpty{
		Date: int32(time.Now().Unix()),
		Seq:  1,
	})
	return &diff, nil
}

type usersService struct {
	gen.UnimplementedUsersServer
}

func (s *usersService) GetUsers(ctx context.Context, req *gen.UsersGetUsersRequest) ([]gen.UserType, error) {
	_ = req
	userID := tlrpc.UserIDFromContext(ctx)
	if userID == 0 {
		userID = 1
	}
	return []gen.UserType{&gen.UserEmpty{ID: userID}}, nil
}

type authService struct {
	gen.UnimplementedAuthServer
}

func (s *authService) SendCode(context.Context, *gen.AuthSendCodeRequest) (*gen.AuthSentCodeType, error) {
	timeout := int32(60)
	sent := gen.AuthSentCodeType(&gen.AuthSentCode{
		Type_:         &gen.AuthSentCodeTypeApp{Length: 5},
		PhoneCodeHash: "compat-phone-code-hash",
		Timeout:       &timeout,
	})
	return &sent, nil
}

func (s *authService) SignIn(ctx context.Context, req *gen.AuthSignInRequest) (*gen.AuthAuthorizationType, error) {
	_ = req
	userID := int64(1)
	if sess := tlrpc.SessionFromContext(ctx); sess != nil {
		sess.UserID = userID
	}
	auth := gen.AuthAuthorizationType(&gen.AuthAuthorization{
		User: &gen.UserEmpty{ID: userID},
	})
	return &auth, nil
}

func traceInterceptor(trace bool) tlrpc.UnaryInterceptor {
	return func(ctx context.Context, req interface{}, info *tlrpc.UnaryServerInfo, handler tlrpc.UnaryHandler) (interface{}, error) {
		if trace {
			method := ""
			tlName := ""
			constructor := uint32(0)
			if obj, ok := req.(interface{ Method() string }); ok {
				method = obj.Method()
			}
			if obj, ok := req.(interface{ TLName() string }); ok {
				tlName = obj.TLName()
			}
			if obj, ok := req.(interface{ ConstructorID() uint32 }); ok {
				constructor = obj.ConstructorID()
			}
			log.Printf("TRACE method=%s tl_name=%s constructor=0x%08x layer=%d user_id=%d auth_key_id=%d full_method=%s", method, tlName, constructor, tlrpc.LayerFromContext(ctx), tlrpc.UserIDFromContext(ctx), tlrpc.AuthKeyIDFromContext(ctx), info.FullMethod)
		}
		return handler(ctx, req)
	}
}

func main() {
	var tcpAddr string
	var wsAddr string
	var maxLayer int
	var wsSecretHex string
	var trace bool
	flag.StringVar(&tcpAddr, "tcp", "127.0.0.1:8080", "TCP listen address")
	flag.StringVar(&wsAddr, "ws", "127.0.0.1:8081", "WebSocket listen address")
	flag.IntVar(&maxLayer, "max-layer", 217, "maximum supported API layer")
	flag.StringVar(&wsSecretHex, "ws-secret-hex", "", "hex obfuscated2 secret for WebSocket")
	flag.BoolVar(&trace, "trace", true, "enable inbound request trace logs")
	flag.Parse()

	var wsSecret []byte
	if wsSecretHex != "" {
		decoded, err := hex.DecodeString(wsSecretHex)
		if err != nil {
			log.Fatalf("invalid ws secret hex: %v", err)
		}
		wsSecret = decoded
	}

	host, portStr, err := splitHostPort(tcpAddr)
	if err != nil {
		log.Fatalf("invalid tcp addr: %v", err)
	}
	portNum, err := strconv.Atoi(portStr)
	if err != nil {
		log.Fatalf("invalid tcp port: %v", err)
	}

	serverKeys := crypto.NewMemoryServerKeyManager()
	if compatKey, err := compatkeys.ServerKey(); err == nil {
		serverKeys.AddKey(compatKey)
	} else {
		log.Printf("WARN compat server key load failed: %v", err)
	}

	srv := tlrpc.NewServer(
		tlrpc.WithMaxLayer(maxLayer),
		tlrpc.WithLogger(stdLogger{}),
		tlrpc.WithUnaryInterceptor(traceInterceptor(trace)),
		tlrpc.WithServerKeyManager(serverKeys),
	)
	gen.RegisterHelpServer(srv, &helpService{dcIP: host, tcpPort: int32(portNum)})
	gen.RegisterUsersServer(srv, &usersService{})
	gen.RegisterAuthServer(srv, &authService{})
	gen.RegisterUpdatesServer(srv, &updatesService{})

	tcpLis, err := (&transport.TCPTransport{AllowObfuscation: true}).Listen(tcpAddr)
	if err != nil {
		log.Fatalf("tcp listen failed: %v", err)
	}
	wsLis, err := (&transport.WebSocketTransport{Secret: wsSecret}).Listen(wsAddr)
	if err != nil {
		log.Fatalf("ws listen failed: %v", err)
	}

	log.Printf("compat server listening tcp=%s ws=%s max_layer=%d trace=%v", tcpAddr, wsAddr, maxLayer, trace)

	go func() {
		if err := srv.ServeTransport(tcpLis); err != nil {
			log.Printf("tcp serve stopped: %v", err)
		}
	}()
	go func() {
		if err := srv.ServeTransport(wsLis); err != nil {
			log.Printf("ws serve stopped: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
	log.Printf("shutting down")
	_ = srv.Stop()
	_ = tcpLis.Close()
	_ = wsLis.Close()
}

func splitHostPort(addr string) (string, string, error) {
	var host string
	var port string
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			host = addr[:i]
			port = addr[i+1:]
			break
		}
	}
	if host == "" || port == "" {
		return "", "", fmt.Errorf("expected host:port")
	}
	return host, port, nil
}
