package compat

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/r6m/tlrpc/compat/client"
	"github.com/r6m/tlrpc/examples/gen"
	"github.com/r6m/tlrpc/internal/compatkeys"
	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/transport"
)

func TestAndroidHandshakeAcknowledgements(t *testing.T) {
	srv := startScenarioServer(t)
	key, err := compatkeys.ServerKey()
	if err != nil {
		t.Fatal(err)
	}
	for _, ws := range []bool{false, true} {
		name := "tcp_obfuscated_abridged"
		if ws {
			name = "websocket_obfuscated_padded"
		}
		t.Run(name, func(t *testing.T) {
			for account := 0; account < 4; account++ {
				t.Run(fmt.Sprintf("account%d", account), func(t *testing.T) {
					t.Parallel()
					var conn transport.Conn
					var err error
					if ws {
						conn, err = (&transport.WebSocketTransport{Protocol: transport.ProtocolPaddedIntermediate}).Dial(srv.wsURL)
					} else {
						conn, err = (&transport.TCPTransport{Protocol: transport.ProtocolAbridged, RequireObfuscation: true}).Dial(srv.tcpAddr)
					}
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = conn.Close() })
					deadline := time.Now().Add(10 * time.Second)
					if err := conn.SetReadDeadline(deadline); err != nil {
						t.Fatal(err)
					}
					if err := conn.SetWriteDeadline(deadline); err != nil {
						t.Fatal(err)
					}
					wire := &androidAcknowledgingConn{Conn: conn}
					cli := client.New(wire, client.WithServerKey(key), client.WithConstructors(gen.GetStaticConstructors()))
					ctx, cancel := context.WithDeadline(context.Background(), deadline)
					defer cancel()
					if _, err := cli.Handshake(ctx); err != nil {
						t.Fatalf("Android handshake: %v", err)
					}
					if wire.acknowledgements != 3 {
						t.Fatalf("handshake ACKs = %d, want resPQ, server_DH_params_ok and dh_gen_ok", wire.acknowledgements)
					}
					response, err := cli.InvokeWrapped(ctx, 217, defaultInitParams(), &gen.HelpGetConfigRequest{}, false)
					if err != nil {
						t.Fatalf("encrypted RPC after final plaintext ACK: %v", err)
					}
					if _, ok := response.(*gen.Config); !ok {
						t.Fatalf("response = %T, want Config", response)
					}
				})
			}
		})
	}
}

// Insert the same plaintext acknowledgements Android sends between key-exchange
// steps. The ordinary compatibility client exercises the no-ACK Web sequence.
type androidAcknowledgingConn struct {
	transport.Conn
	acknowledgements int
}

func (c *androidAcknowledgingConn) ReadMessage(limit int) ([]byte, error) {
	frame, err := c.Conn.ReadMessage(limit)
	if err != nil || len(frame) < 8 || binary.LittleEndian.Uint64(frame) != 0 {
		return frame, err
	}
	var message mtproto.UnencryptedMessage
	if err := message.Deserialize(frame); err != nil {
		return nil, err
	}
	var body bytes.Buffer
	if err := (&mtprototl.MsgsAck{MsgIDs: []int64{message.MsgID}}).SerializeTL(&body); err != nil {
		return nil, err
	}
	ack := mtproto.UnencryptedMessage{MsgID: time.Now().Unix()<<32 | int64(c.acknowledgements+1)*4, Data: body.Bytes()}
	encoded, err := ack.Serialize()
	if err != nil {
		return nil, err
	}
	if err := c.WriteMessage(encoded); err != nil {
		return nil, err
	}
	c.acknowledgements++
	return frame, nil
}
