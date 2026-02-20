//go:build gotd_integ

package transport_test

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gotd/td/bin"
	gotdcrypto "github.com/gotd/td/crypto"
	tdsession "github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/dcs"
	"github.com/gotd/td/tg"
	gotdtransport "github.com/gotd/td/transport"
	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/examples/gen"
	"github.com/r6m/tlrpc/internal/compatkeys"
	"github.com/r6m/tlrpc/mtproto"
	tlrpcsession "github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
	"go.uber.org/zap"
)

// Run with:
//
//	go test -tags=gotd_integ ./transport -run TestGotdHandshakeMatrix -v
func TestGotdHandshakeMatrix(t *testing.T) {
	tests := []struct {
		name     string
		protocol dcs.Protocol
	}{
		{name: "abridged", protocol: gotdtransport.Abridged},
		{name: "intermediate", protocol: gotdtransport.Intermediate},
		{name: "padded_intermediate", protocol: gotdtransport.PaddedIntermediate},
		{name: "full", protocol: gotdtransport.Full},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := runGotdHandshakeCase(t, tc.protocol)
			if err == nil {
				return
			}
			if tc.name == "full" && shouldSkipFull(err) {
				t.Skipf("gotd full transport appears unsupported in this environment: %s", errorChain(err))
			}
			t.Fatalf("protocol=%s handshake/config failed: %s", tc.name, errorChain(err))
		})
	}
}

func runGotdHandshakeCase(t *testing.T, protocol dcs.Protocol) error {
	t.Helper()

	log := &testLogger{t: t}

	compatKey, err := compatkeys.ServerKey()
	if err != nil {
		return fmt.Errorf("load compat key: %w", err)
	}
	gotdFingerprint := telegram.PublicKey{RSA: &compatKey.Key.PublicKey}.Fingerprint()
	if gotdFingerprint != compatKey.ID {
		return fmt.Errorf("compat key fingerprint mismatch: got %d want %d", compatKey.ID, gotdFingerprint)
	}
	serverKeys := crypto.NewMemoryServerKeyManager()
	serverKeys.AddKey(compatKey)

	authKeys := crypto.NewMemoryAuthKeyManager()
	sessions := tlrpcsession.NewMemoryManager()

	srv := tlrpc.NewServer(
		tlrpc.WithAuthKeyManager(authKeys),
		tlrpc.WithSessionManager(sessions),
		tlrpc.WithServerKeyManager(serverKeys),
		tlrpc.WithMaxLayer(217),
		tlrpc.WithUnaryInterceptor(tlrpc.LoggingInterceptor(log)),
		tlrpc.WithLogger(log),
	)

	gen.RegisterHelpServer(srv, &gotdHelpService{})

	lis, err := (&transport.TCPTransport{AllowObfuscation: true}).Listen("127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	go func() { _ = srv.ServeTransport(lis) }()
	t.Cleanup(func() {
		_ = srv.Stop()
		_ = lis.Close()
	})

	addr := lis.Addr().(*net.TCPAddr)
	dc := tg.DCOption{
		ID:           1,
		IPAddress:    addr.IP.String(),
		Port:         int(addr.Port),
		ThisPortOnly: true,
	}
	dcList := dcs.List{Options: []tg.DCOption{dc}}

	opts := telegram.Options{
		DCList: dcList,
		PublicKeys: []telegram.PublicKey{
			{RSA: &compatKey.Key.PublicKey},
		},
		Resolver: dcs.Plain(dcs.PlainOptions{
			Protocol:     protocol,
			NoObfuscated: true,
		}),
		SessionStorage:  &tdsession.StorageMemory{},
		DC:              1,
		ExchangeTimeout: 10 * time.Second,
		DialTimeout:     5 * time.Second,
		Logger:          zap.NewExample(),
	}

	client := telegram.NewClient(1, "test", opts)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.Run(ctx, func(ctx context.Context) error {
		_, err := client.API().HelpGetConfig(ctx)
		return err
	}); err != nil {
		return fmt.Errorf("client run help.getConfig: %w", err)
	}

	if slices.Contains(log.MissingConstructors(), "0xb921bd04") {
		return errors.New("server reported METHOD_NOT_FOUND for get_future_salts (0xb921bd04)")
	}

	return nil
}

func shouldSkipFull(err error) bool {
	s := strings.ToLower(errorChain(err))
	return strings.Contains(s, "unsupported") && strings.Contains(s, "full")
}

func errorChain(err error) string {
	if err == nil {
		return "<nil>"
	}
	parts := make([]string, 0, 4)
	for e := err; e != nil; e = errors.Unwrap(e) {
		parts = append(parts, e.Error())
	}
	return strings.Join(parts, " | ")
}

type gotdHelpService struct {
	gen.UnimplementedHelpServer

	mu        sync.Mutex
	lastLayer int
}

func (s *gotdHelpService) GetConfig(ctx context.Context, _ *gen.HelpGetConfigRequest) (*gen.Config, error) {
	s.mu.Lock()
	s.lastLayer = tlrpc.LayerFromContext(ctx)
	s.mu.Unlock()

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

func (s *gotdHelpService) LastLayer() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastLayer
}

func TestGotdWrappedFlow(t *testing.T) {
	log := &testLogger{t: t}
	helpSvc := &gotdHelpService{}

	compatKey, err := compatkeys.ServerKey()
	if err != nil {
		t.Fatalf("load compat key: %v", err)
	}
	serverKeys := crypto.NewMemoryServerKeyManager()
	serverKeys.AddKey(compatKey)

	srv := tlrpc.NewServer(
		tlrpc.WithAuthKeyManager(crypto.NewMemoryAuthKeyManager()),
		tlrpc.WithSessionManager(tlrpcsession.NewMemoryManager()),
		tlrpc.WithServerKeyManager(serverKeys),
		tlrpc.WithMaxLayer(217),
		tlrpc.WithUnaryInterceptor(tlrpc.LoggingInterceptor(log)),
		tlrpc.WithLogger(log),
	)
	gen.RegisterHelpServer(srv, helpSvc)

	lis, err := (&transport.TCPTransport{AllowObfuscation: true}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.ServeTransport(lis) }()
	t.Cleanup(func() {
		_ = srv.Stop()
		_ = lis.Close()
	})

	addr := lis.Addr().(*net.TCPAddr)
	dcList := dcs.List{Options: []tg.DCOption{{
		ID:           1,
		IPAddress:    addr.IP.String(),
		Port:         int(addr.Port),
		ThisPortOnly: true,
	}}}

	opts := telegram.Options{
		DCList: dcList,
		PublicKeys: []telegram.PublicKey{
			{RSA: &compatKey.Key.PublicKey},
		},
		Resolver: dcs.Plain(dcs.PlainOptions{
			Protocol:     gotdtransport.Intermediate,
			NoObfuscated: true,
		}),
		SessionStorage:  &tdsession.StorageMemory{},
		DC:              1,
		ExchangeTimeout: 10 * time.Second,
		DialTimeout:     5 * time.Second,
		Logger:          zap.NewExample(),
	}

	client := telegram.NewClient(1, "test", opts)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.Run(ctx, func(ctx context.Context) error {
		req := &tg.InvokeWithLayerRequest{
			Layer: 217,
			Query: &tg.InitConnectionRequest{
				APIID:          1,
				DeviceModel:    "gotd",
				SystemVersion:  "test",
				AppVersion:     "1.0",
				SystemLangCode: "en",
				LangPack:       "",
				LangCode:       "en",
				Query: &tg.InvokeWithoutUpdatesRequest{
					Query: &tg.HelpGetConfigRequest{},
				},
			},
		}
		var out tg.Config
		if err := client.Invoke(ctx, req, &out); err != nil {
			return err
		}
		if out.ThisDC != 1 {
			return fmt.Errorf("unexpected config.this_dc: got %d", out.ThisDC)
		}
		return nil
	}); err != nil {
		t.Fatalf("wrapped invoke failed: %s", errorChain(err))
	}

	if slices.Contains(log.MissingConstructors(), "0xda9b0d0d") ||
		slices.Contains(log.MissingConstructors(), "0xc1cd5ea9") ||
		slices.Contains(log.MissingConstructors(), "0xbf9459b7") {
		t.Fatalf("wrapper constructors should be handled, missing=%v", log.MissingConstructors())
	}

	if got := helpSvc.LastLayer(); got != 217 {
		t.Fatalf("expected handler layer=217, got %d", got)
	}
}

func TestGotdBadServerSaltRecovery(t *testing.T) {
	log := &testLogger{t: t}
	hooks := &tlrpc.GotdTestHooks{}
	hooks.ForceBadServerSaltOnce.Store(true)
	helpSvc := &gotdHelpService{}

	compatKey, err := compatkeys.ServerKey()
	if err != nil {
		t.Fatalf("load compat key: %v", err)
	}
	serverKeys := crypto.NewMemoryServerKeyManager()
	serverKeys.AddKey(compatKey)

	srv := tlrpc.NewServer(
		tlrpc.WithAuthKeyManager(crypto.NewMemoryAuthKeyManager()),
		tlrpc.WithSessionManager(tlrpcsession.NewMemoryManager()),
		tlrpc.WithServerKeyManager(serverKeys),
		tlrpc.WithMaxLayer(217),
		tlrpc.WithUnaryInterceptor(tlrpc.LoggingInterceptor(log)),
		tlrpc.WithLogger(log),
		tlrpc.WithGotdTestHooks(hooks),
	)
	gen.RegisterHelpServer(srv, helpSvc)

	lis, err := (&transport.TCPTransport{AllowObfuscation: true}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.ServeTransport(lis) }()
	t.Cleanup(func() {
		_ = srv.Stop()
		_ = lis.Close()
	})

	addr := lis.Addr().(*net.TCPAddr)
	dcList := dcs.List{Options: []tg.DCOption{{
		ID:           1,
		IPAddress:    addr.IP.String(),
		Port:         int(addr.Port),
		ThisPortOnly: true,
	}}}
	client := telegram.NewClient(1, "test", telegram.Options{
		DCList: dcList,
		PublicKeys: []telegram.PublicKey{
			{RSA: &compatKey.Key.PublicKey},
		},
		Resolver: dcs.Plain(dcs.PlainOptions{
			Protocol:     gotdtransport.Intermediate,
			NoObfuscated: true,
		}),
		SessionStorage:  &tdsession.StorageMemory{},
		DC:              1,
		ExchangeTimeout: 10 * time.Second,
		DialTimeout:     5 * time.Second,
		Logger:          zap.NewExample(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := client.Run(ctx, func(ctx context.Context) error {
		_, err := client.API().HelpGetConfig(ctx)
		return err
	}); err != nil {
		t.Fatalf("help.getConfig with bad_server_salt recovery failed: %s", errorChain(err))
	}

	if got := hooks.EncryptedRequestCount.Load(); got < 2 {
		t.Fatalf("expected retry after bad_server_salt, encrypted requests=%d", got)
	}
	if hooks.ForceBadServerSaltOnce.Load() {
		t.Fatalf("expected bad_server_salt hook to be consumed")
	}
	if slices.Contains(log.MissingConstructors(), "0xedab447b") {
		t.Fatalf("bad_server_salt must be handled by gotd, missing=%v", log.MissingConstructors())
	}
}

type testLogger struct {
	t *testing.T

	mu      sync.Mutex
	missing []string
}

func (l *testLogger) Info(msg string, args ...interface{}) { l.t.Logf("INFO: %s %v", msg, args) }

func (l *testLogger) Error(msg string, args ...interface{}) {
	l.t.Logf("ERROR: %s %v", msg, args)
	if msg != "method not found" {
		return
	}
	for i := 0; i+1 < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok || key != "constructor_id" {
			continue
		}
		val, ok := args[i+1].(string)
		if !ok {
			continue
		}
		l.mu.Lock()
		l.missing = append(l.missing, val)
		l.mu.Unlock()
	}
}

func (l *testLogger) Debug(msg string, args ...interface{}) { l.t.Logf("DEBUG: %s %v", msg, args) }

func (l *testLogger) MissingConstructors() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.missing))
	copy(out, l.missing)
	return out
}

type rawMsg []byte

func (r rawMsg) Encode(b *bin.Buffer) error {
	b.Put(r)
	return nil
}

func TestKDFCompat(t *testing.T) {
	var authKeyBytes [256]byte
	if _, err := rand.Read(authKeyBytes[:]); err != nil {
		t.Fatalf("rand authKey: %v", err)
	}
	plaintext := make([]byte, 64)
	if _, err := rand.Read(plaintext); err != nil {
		t.Fatalf("rand plaintext: %v", err)
	}

	var key gotdcrypto.Key
	copy(key[:], authKeyBytes[:])

	gotdID := key.ID()
	ourID := crypto.ComputeAuthKeyID(authKeyBytes[:])
	var ourIDBytes [8]byte
	binary.LittleEndian.PutUint64(ourIDBytes[:], uint64(ourID))
	if string(gotdID[:]) != string(ourIDBytes[:]) {
		t.Fatalf("auth_key_id mismatch with gotd")
	}

	msgKeyGotd := gotdcrypto.MessageKey(key, plaintext, gotdcrypto.Client)
	msgKeyOur := crypto.ComputeMsgKey(authKeyBytes[:], plaintext, true)
	if string(msgKeyGotd[:]) != string(msgKeyOur[:]) {
		t.Fatalf("msg_key mismatch with gotd")
	}

	kGotd, ivGotd := gotdcrypto.Keys(key, msgKeyGotd, gotdcrypto.Client)
	kOur, ivOur := crypto.ComputeKDF(authKeyBytes[:], msgKeyOur, true)
	if string(kGotd[:]) != string(kOur) || string(ivGotd[:]) != string(ivOur) {
		t.Fatalf("kdf mismatch with gotd")
	}
}

func TestDecryptCompat(t *testing.T) {
	var authKeyBytes [256]byte
	if _, err := rand.Read(authKeyBytes[:]); err != nil {
		t.Fatalf("rand authKey: %v", err)
	}
	var key gotdcrypto.Key
	copy(key[:], authKeyBytes[:])
	auth := gotdcrypto.AuthKey{Value: key, ID: key.ID()}

	data := gotdcrypto.EncryptedMessageData{
		Salt:      1,
		SessionID: 2,
		MessageID: 3,
		SeqNo:     1,
		Message:   rawMsg([]byte("ping")),
	}

	var buf bin.Buffer
	cipher := gotdcrypto.NewClientCipher(rand.Reader)
	if err := cipher.Encrypt(auth, data, &buf); err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	var msg gotdcrypto.EncryptedMessage
	if err := msg.Decode(&buf); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var keyID crypto.KeyID
	keyID = crypto.KeyID(binary.LittleEndian.Uint64(msg.AuthKeyID[:]))
	ourAuth := crypto.AuthKey{}
	copy(ourAuth[:], authKeyBytes[:])
	enc := &mtproto.EncryptedMessage{
		AuthKeyID:     keyID,
		MsgKey:        [16]byte(msg.MsgKey),
		EncryptedData: msg.EncryptedData,
	}
	if _, err := enc.DecryptFromClient(ourAuth); err != nil {
		t.Fatalf("decrypt: %v", err)
	}
}
