package compat

import (
	"context"
	"testing"
	"time"

	"github.com/r6m/tlrpc/examples/gen"
)

func TestScenarioLoginTCPAndWS(t *testing.T) {
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
			defer cli.Close()

			userID := handshakeAndLogin(t, cli, 217)

			sess, err := srv.sessions.Get(cli.Session().AuthKeyID)
			if err != nil {
				t.Fatalf("session lookup: %v", err)
			}
			if sess.UserID != userID {
				t.Fatalf("session user mismatch: got %d want %d", sess.UserID, userID)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Send empty vector to avoid generator InputUserType deserialization issues.
			resp, err := cli.InvokeWrapped(ctx, 217, defaultInitParams(), &gen.UsersGetUsersRequest{}, false)
			if err != nil {
				t.Fatalf("users.getUsers: %v", err)
			}
			vec, ok := resp.(*userVector)
			if !ok {
				t.Fatalf("unexpected response type %T", resp)
			}
			if len(vec.Items) != 1 {
				t.Fatalf("expected 1 user, got %d", len(vec.Items))
			}
			switch u := vec.Items[0].(type) {
			case *gen.User:
				if u.ID != userID {
					t.Fatalf("user id mismatch: %d", u.ID)
				}
			case *gen.UserEmpty:
				if u.ID != userID {
					t.Fatalf("user id mismatch: %d", u.ID)
				}
			default:
				t.Fatalf("unexpected user type %T", vec.Items[0])
			}
		})
	}
}
