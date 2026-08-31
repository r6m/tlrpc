package main

import (
	"context"
	"log"

	"github.com/r6m/tlrpc"
	echo "github.com/r6m/tlrpc/examples/echo/gen"
	"github.com/r6m/tlrpc/transport"
)

type echoServer struct {
	echo.UnimplementedEchoServer
}

func (s *echoServer) Echo(ctx context.Context, req *echo.EchoEchoRequest) (*echo.EchoResponse, error) {
	if sender, ok := tlrpc.SenderFromContext(ctx); ok {
		_ = sender.Send(ctx, &echo.EchoUpdate{Message: "server push: " + req.Message})
	}
	return &echo.EchoResponse{Message: req.Message}, nil
}

func main() {
	tcp := &transport.TCPTransport{}
	ws := &transport.WebSocketTransport{}
	srv := tlrpc.NewServer()
	echo.RegisterEchoServer(srv, &echoServer{})

	tcpLis, err := tcp.Listen(":9000")
	if err != nil {
		log.Fatalf("tcp listen: %v", err)
	}
	defer func() { _ = tcpLis.Close() }()

	wsLis, err := ws.Listen(":9001")
	if err != nil {
		log.Fatalf("ws listen: %v", err)
	}
	defer func() { _ = wsLis.Close() }()

	errCh := make(chan error, 2)
	go func() { errCh <- srv.ServeTransport(tcpLis) }()
	go func() { errCh <- srv.ServeTransport(wsLis) }()

	log.Printf("echo TCP listening on %s", tcpLis.Addr())
	log.Printf("echo WS listening on %s", wsLis.Addr())

	if err := <-errCh; err != nil {
		log.Fatalf("serve: %v", err)
	}
}
