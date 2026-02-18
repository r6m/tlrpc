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
	return &echo.EchoResponse{Message: req.Message}, nil
}

func main() {
	t := &transport.TCPTransport{}
	w := &transport.WebSocketTransport{}
	srv := tlrpc.NewServer(
		tlrpc.WithTransport(t),
		tlrpc.WithTransport(w),
	)
	echo.RegisterEchoServer(srv, &echoServer{})

	lis, err := t.Listen(":9000")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("echo server listening on %s", lis.Addr())
	if err := srv.ServeTransport(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
