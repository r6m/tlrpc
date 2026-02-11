package tlrpc

import (
	"context"
	"net"
	"sync"
)

// connHandler handles a single client connection.
type connHandler struct {
	server *Server
	conn   net.Conn
}

// newConnHandler creates a new connection handler.
func newConnHandler(server *Server, conn net.Conn) *connHandler {
	return &connHandler{
		server: server,
		conn:   conn,
	}
}

// run starts handling the connection.
func (h *connHandler) run() {
	defer h.conn.Close()

	// TODO: Implement MTProto handshake
	// TODO: Implement message loop
	// For now, just close the connection
}