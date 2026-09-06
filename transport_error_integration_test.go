package tlrpc_test

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/transport"
	mtprotocodec "github.com/r6m/tlrpc/transport/mtproto_codec"
)

func TestAuthKeyLookupErrorsOverRealTransports(t *testing.T) {
	transports := []struct {
		name   string
		listen func() (transport.Listener, error)
		dial   func(transport.Listener) (transport.Conn, error)
	}{
		{
			name: "tcp_abridged",
			listen: func() (transport.Listener, error) {
				return (&transport.TCPTransport{}).Listen("127.0.0.1:0")
			},
			dial: func(listener transport.Listener) (transport.Conn, error) {
				return (&transport.TCPTransport{Protocol: transport.ProtocolAbridged}).Dial(listener.Addr().String())
			},
		},
		{
			name: "websocket_obfuscated_abridged",
			listen: func() (transport.Listener, error) {
				return (&transport.WebSocketTransport{}).Listen("127.0.0.1:0")
			},
			dial: func(listener transport.Listener) (transport.Conn, error) {
				return (&transport.WebSocketTransport{Protocol: transport.ProtocolAbridged}).Dial("ws://" + listener.Addr().String())
			},
		},
	}

	lookupFailures := []struct {
		name              string
		err               error
		wantTransportCode int32
	}{
		{name: "missing", err: crypto.ErrAuthKeyNotFound, wantTransportCode: -404},
		{name: "transient_store_failure", err: errors.New("database temporarily unavailable")},
	}

	for _, transportCase := range transports {
		for _, failure := range lookupFailures {
			t.Run(transportCase.name+"/"+failure.name, func(t *testing.T) {
				listener, err := transportCase.listen()
				if err != nil {
					t.Fatalf("listen: %v", err)
				}
				server := tlrpc.NewServer(
					tlrpc.WithAuthKeyManager(authKeyFailureManager{err: failure.err}),
					tlrpc.WithLogger(discardLogger{}),
				)
				serveDone := make(chan error, 1)
				go func() { serveDone <- server.ServeTransport(listener) }()
				t.Cleanup(func() {
					_ = server.Stop()
					_ = listener.Close()
					if serveErr := <-serveDone; serveErr != nil {
						t.Errorf("ServeTransport: %v", serveErr)
					}
				})

				client, err := transportCase.dial(listener)
				if err != nil {
					t.Fatalf("dial: %v", err)
				}
				t.Cleanup(func() { _ = client.Close() })
				if err := client.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
					t.Fatalf("set read deadline: %v", err)
				}
				if err := client.WriteMessage(unknownAuthKeyFrame(0x0102030405060708)); err != nil {
					t.Fatalf("write unknown auth key frame: %v", err)
				}

				_, readErr := client.ReadMessage(0)
				var transportErr mtprotocodec.TransportError
				if failure.wantTransportCode != 0 {
					if !errors.As(readErr, &transportErr) || transportErr.Code != failure.wantTransportCode {
						t.Fatalf("first read error = %v, want transport error %d", readErr, failure.wantTransportCode)
					}
					_, closeErr := client.ReadMessage(0)
					if closeErr == nil || errors.As(closeErr, &transportErr) {
						t.Fatalf("second read error = %v, want closed transport", closeErr)
					}
					return
				}
				if readErr == nil {
					t.Fatal("first read succeeded after transient auth key source failure")
				}
				if errors.As(readErr, &transportErr) {
					t.Fatalf("first read error = %v, must not expose a transport error", readErr)
				}
			})
		}
	}
}

func unknownAuthKeyFrame(keyID crypto.KeyID) []byte {
	frame := make([]byte, 24)
	binary.LittleEndian.PutUint64(frame, uint64(keyID))
	return frame
}

type authKeyFailureManager struct {
	err error
}

func (m authKeyFailureManager) Get(crypto.KeyID) (crypto.AuthKey, error) {
	return crypto.AuthKey{}, m.err
}

func (authKeyFailureManager) Put(crypto.KeyID, crypto.AuthKey) error { return nil }
func (authKeyFailureManager) Delete(crypto.KeyID) error              { return nil }

type discardLogger struct{}

func (discardLogger) Info(string, ...interface{})  {}
func (discardLogger) Error(string, ...interface{}) {}
func (discardLogger) Debug(string, ...interface{}) {}
