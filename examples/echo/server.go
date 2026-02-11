package main

import (
	"context"
	"fmt"
	"log"
	"net"

	"github.com/yourorg/tlrpc"
)

func main() {
	// Create server
	server := tlrpc.NewServer()

	// TODO: Register echo service
	// gen.RegisterEchoServer(server, &echoService{})

	// Listen
	lis, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Echo server listening on :8080")
	if err := server.Serve(lis); err != nil {
		log.Fatal(err)
	}
}

// echoService implements the echo service
type echoService struct{}

// Echo implements the Echo method
func (s *echoService) Echo(ctx context.Context, req *EchoRequest) (*EchoResponse, error) {
	return &EchoResponse{
		Message: fmt.Sprintf("Echo: %s", req.Message),
	}, nil
}

// EchoRequest represents an echo request
type EchoRequest struct {
	Message string
}

// EchoResponse represents an echo response
type EchoResponse struct {
	Message string
}