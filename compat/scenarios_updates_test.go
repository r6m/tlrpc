package compat

import (
	"context"
	"testing"
	"time"

	"github.com/r6m/tlrpc/examples/gen"
)

func TestScenarioUpdatesBaselineAndPushTCPAndWS(t *testing.T) {
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

			userID := handshakeAndLogin(t, cli, 217)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			stateObj, err := cli.InvokeWrapped(ctx, 217, defaultInitParams(), &gen.UpdatesGetStateRequest{}, false)
			if err != nil {
				t.Fatalf("updates.getState: %v", err)
			}
			state := stateObj.(*gen.UpdatesState)

			update := &gen.UpdateUserStatus{UserID: userID, Status: &gen.UserStatusOnline{Expires: int32(time.Now().Unix() + 60)}}
			if _, err := srv.updates.publish(srv.srv, userID, update); err != nil {
				t.Fatalf("publish update: %v", err)
			}

			readCtx, cancelRead := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancelRead()
			pushed, err := cli.ReadOne(readCtx)
			if err == nil {
				switch pushed.(type) {
				case *gen.Updates, *gen.UpdatesTooLong, *updatesLite:
					return
				default:
					t.Fatalf("unexpected pushed type %T", pushed)
				}
			}

			diffReq := &gen.UpdatesGetDifferenceRequest{Pts: state.Pts, Date: state.Date, Qts: state.Qts}
			diffObj, err := cli.InvokeWrapped(ctx, 217, defaultInitParams(), diffReq, false)
			if err != nil {
				t.Fatalf("updates.getDifference: %v", err)
			}
			switch diff := diffObj.(type) {
			case *gen.UpdatesDifference:
				if len(diff.OtherUpdates) == 0 {
					t.Fatalf("expected updates in difference")
				}
			case *gen.UpdatesDifferenceEmpty:
				t.Fatalf("unexpected empty difference")
			case *gen.UpdatesDifferenceTooLong:
				// acceptable fallback
			case *updatesDifferenceLite:
				if len(diff.OtherUpdates) == 0 {
					t.Fatalf("expected updates in difference")
				}
			default:
				t.Fatalf("unexpected difference type %T", diffObj)
			}
		})
	}
}

func TestScenarioReconnectGetDifferenceTCPAndWS(t *testing.T) {
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
			userID := handshakeAndLogin(t, cli, 217)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stateObj, err := cli.InvokeWrapped(ctx, 217, defaultInitParams(), &gen.UpdatesGetStateRequest{}, false)
			if err != nil {
				t.Fatalf("updates.getState: %v", err)
			}
			state := stateObj.(*gen.UpdatesState)

			_ = cli.Close()

			update := &gen.UpdateUserStatus{UserID: userID, Status: &gen.UserStatusOnline{Expires: int32(time.Now().Unix() + 120)}}
			if _, err := srv.updates.publish(srv.srv, userID, update); err != nil {
				t.Fatalf("publish update: %v", err)
			}

			cli = newScenarioClient(t, srv.tcpAddr, srv.wsURL, tc.ws)
			defer func() { _ = cli.Close() }()
			_ = handshakeAndLogin(t, cli, 217)

			diffReq := &gen.UpdatesGetDifferenceRequest{Pts: state.Pts, Date: state.Date, Qts: state.Qts}
			diffObj, err := cli.InvokeWrapped(ctx, 217, defaultInitParams(), diffReq, false)
			if err != nil {
				t.Fatalf("updates.getDifference: %v", err)
			}
			switch diff := diffObj.(type) {
			case *gen.UpdatesDifference:
				if len(diff.OtherUpdates) == 0 {
					t.Fatalf("expected updates in difference")
				}
			case *gen.UpdatesDifferenceEmpty:
				t.Fatalf("unexpected empty difference")
			case *gen.UpdatesDifferenceTooLong:
				// acceptable fallback
			case *updatesDifferenceLite:
				if len(diff.OtherUpdates) == 0 {
					t.Fatalf("expected updates in difference")
				}
			default:
				t.Fatalf("unexpected difference type %T", diffObj)
			}
		})
	}
}

func TestScenarioInvokeWithoutUpdatesTCPAndWS(t *testing.T) {
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

			userID := handshakeAndLogin(t, cli, 217)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stateObj, err := cli.InvokeWrapped(ctx, 217, defaultInitParams(), &gen.UpdatesGetStateRequest{}, false)
			if err != nil {
				t.Fatalf("updates.getState: %v", err)
			}
			state := stateObj.(*gen.UpdatesState)

			_, err = cli.InvokeWrapped(ctx, 217, defaultInitParams(), &gen.HelpGetConfigRequest{}, true)
			if err != nil {
				t.Fatalf("invoke without updates: %v", err)
			}

			update := &gen.UpdateUserStatus{UserID: userID, Status: &gen.UserStatusOnline{Expires: int32(time.Now().Unix() + 120)}}
			if _, err := srv.updates.publish(srv.srv, userID, update); err != nil {
				t.Fatalf("publish update: %v", err)
			}

			readCtx, cancelRead := context.WithTimeout(context.Background(), 400*time.Millisecond)
			defer cancelRead()
			if _, err := cli.ReadOne(readCtx); err == nil {
				t.Fatalf("expected no pushed updates")
			}
			_ = cli.Close()

			cli = newScenarioClient(t, srv.tcpAddr, srv.wsURL, tc.ws)
			defer func() { _ = cli.Close() }()
			_ = handshakeAndLogin(t, cli, 217)

			diffReq := &gen.UpdatesGetDifferenceRequest{Pts: state.Pts, Date: state.Date, Qts: state.Qts}
			diffObj, err := cli.InvokeWrapped(ctx, 217, defaultInitParams(), diffReq, false)
			if err != nil {
				t.Fatalf("updates.getDifference: %v", err)
			}
			switch diff := diffObj.(type) {
			case *gen.UpdatesDifference:
				if len(diff.OtherUpdates) == 0 {
					t.Fatalf("expected updates in difference")
				}
			case *gen.UpdatesDifferenceEmpty:
				t.Fatalf("unexpected empty difference")
			case *updatesDifferenceLite:
				if len(diff.OtherUpdates) == 0 {
					t.Fatalf("expected updates in difference")
				}
			default:
				t.Fatalf("unexpected difference type %T", diffObj)
			}
		})
	}
}
