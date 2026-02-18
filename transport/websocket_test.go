package transport

import "testing"

func TestWebSocketTransport(t *testing.T) {
	// WebSocket transport requires a network listener; skip in unit tests.
	t.Skip("requires network listener")
}
