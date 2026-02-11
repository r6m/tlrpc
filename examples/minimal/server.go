package main

import (
	"context"
	"fmt"
	"log"
	"net"

	"github.com/r6m/tlrpc"
)

func main() {
	// Create minimal server
	server := tlrpc.NewServer(
		tlrpc.WithMaxLayer(222),
	)

	// TODO: Register minimal service
	// gen.RegisterMinimalServer(server, &minimalService{})

	// Listen
	lis, err := net.Listen("tcp", ":8081")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Minimal server listening on :8081")
	if err := server.Serve(lis); err != nil {
		log.Fatal(err)
	}
}

// minimalService implements minimal functionality
type minimalService struct{}

// Ping implements the Ping method
func (s *minimalService) Ping(ctx context.Context, req *PingRequest) (*PingResponse, error) {
	return &PingResponse{
		Pong: req.Ping + 1,
	}, nil
}

// PingRequest represents a ping request
type PingRequest struct {
	Ping int64
}

// PingResponse represents a ping response
type PingResponse struct {
	Pong int64
}
