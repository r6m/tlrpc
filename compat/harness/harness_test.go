package harness

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/r6m/tlrpc/examples/gen"
)

func TestHarnessHandshakeRPCAndError(t *testing.T) {
	srv, err := Start()
	if err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	cli, err := DialTCP(srv.TCPAddr, nil)
	if err != nil {
		t.Fatalf("dial client: %v", err)
	}
	defer func() { _ = cli.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := cli.Handshake(ctx); err != nil {
		t.Fatalf("handshake: %v", err)
	}

	resp, err := cli.InvokeWrapped(ctx, 217, defaultInitParams(), &gen.HelpGetConfigRequest{}, false)
	if err != nil {
		t.Fatalf("help.getConfig: %v", err)
	}
	cfg, ok := resp.(*gen.Config)
	if !ok {
		t.Fatalf("unexpected config type %T", resp)
	}
	if cfg.ThisDc == 0 || cfg.ChatSizeMax == 0 {
		t.Fatalf("unexpected config values: %+v", cfg)
	}

	_, err = cli.InvokeWrapped(ctx, 217, defaultInitParams(), &gen.HelpGetNearestDcRequest{}, false)
	if err == nil {
		t.Fatalf("expected rpc_error for help.getNearestDc")
	}
	if !strings.Contains(err.Error(), "NEAREST_DC_UNAVAILABLE") {
		t.Fatalf("unexpected error: %v", err)
	}
}
