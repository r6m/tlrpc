package compat

import (
	"context"
	"testing"
	"time"

	"github.com/r6m/tlrpc/examples/gen"
)

func TestScenarioBootTCPAndWS(t *testing.T) {
	srv := startScenarioServer(t)
	cases := []struct {
		name string
		ws   bool
	}{
		{"tcp", false},
		{"ws", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cli := newScenarioClient(t, srv.tcpAddr, srv.wsURL, tc.ws)
			defer func() { _ = cli.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if _, err := cli.Handshake(ctx); err != nil {
				t.Fatalf("handshake: %v", err)
			}
			resp, err := cli.InvokeWrapped(ctx, 217, defaultInitParams(), &gen.HelpGetConfigRequest{}, false)
			if err != nil {
				t.Fatalf("invoke wrapped: %v", err)
			}
			cfg, ok := resp.(*gen.Config)
			if !ok {
				t.Fatalf("unexpected response type %T", resp)
			}
			if cfg.ThisDc == 0 || cfg.MessageLengthMax == 0 || cfg.DcTxtDomainName == "" {
				t.Fatalf("config missing expected fields: %+v", cfg)
			}
		})
	}
}
