package transport

import (
	"crypto/tls"
	"net/http"
	"testing"
	"time"
)

func TestWebSocketOriginPolicyDefaultsToSameOriginAndAllowsMissing(t *testing.T) {
	transport := &WebSocketTransport{}
	checkOrigin := transport.originChecker()

	if !checkOrigin(&http.Request{Host: "example.com"}) {
		t.Fatal("missing origin was rejected")
	}
	if !checkOrigin(&http.Request{
		Host:   "example.com",
		Header: http.Header{"Origin": []string{"http://example.com"}},
	}) {
		t.Fatal("same origin request was rejected")
	}
	if checkOrigin(&http.Request{
		Host:   "example.com",
		Header: http.Header{"Origin": []string{"https://evil.example"}},
	}) {
		t.Fatal("cross origin request was accepted")
	}
}

func TestWebSocketOriginPolicyAllowsExplicitOrigins(t *testing.T) {
	transport := &WebSocketTransport{
		OriginPolicy: WebSocketOriginPolicy{
			AllowedOrigins: []string{"https://app.example", "https://admin.example"},
		},
	}
	checkOrigin := transport.originChecker()
	if !checkOrigin(&http.Request{
		Host:   "gateway.example",
		Header: http.Header{"Origin": []string{"https://app.example"}},
	}) {
		t.Fatal("configured origin was rejected")
	}
	if checkOrigin(&http.Request{
		Host:   "gateway.example",
		Header: http.Header{"Origin": []string{"https://unknown.example"}},
	}) {
		t.Fatal("unknown origin was accepted")
	}
}

func TestWebSocketOriginPolicyUsesRequestScheme(t *testing.T) {
	transport := &WebSocketTransport{}
	checkOrigin := transport.originChecker()
	if !checkOrigin(&http.Request{
		Host:   "secure.example",
		TLS:    &tls.ConnectionState{},
		Header: http.Header{"Origin": []string{"https://secure.example"}},
	}) {
		t.Fatal("https same-origin request was rejected")
	}
}

func TestWebSocketOriginPolicyCannotBeLoosenedByCustomUpgrader(t *testing.T) {
	transport := &WebSocketTransport{}
	transport.Upgrader.CheckOrigin = func(*http.Request) bool { return true }
	if transport.upgradeOriginChecker()(&http.Request{
		Host:   "gateway.example",
		Header: http.Header{"Origin": []string{"https://evil.example"}},
	}) {
		t.Fatal("custom upgrader loosened the framework origin policy")
	}
}

func TestWebSocketOriginPolicyRejectsMalformedAllowlistEntries(t *testing.T) {
	tests := []string{
		"app.example",
		"ftp://app.example",
		"https://app.example/path",
		"https://user@app.example",
		"https://app.example?token=secret",
	}
	for _, origin := range tests {
		t.Run(origin, func(t *testing.T) {
			transport := &WebSocketTransport{OriginPolicy: WebSocketOriginPolicy{AllowedOrigins: []string{origin}}}
			if err := transport.validateOriginPolicy(); err == nil {
				t.Fatalf("validateOriginPolicy(%q) error = nil", origin)
			}
		})
	}
	if err := (&WebSocketTransport{OriginPolicy: WebSocketOriginPolicy{
		AllowedOrigins: []string{"http://[::1]:5178", "https://app.example"},
	}}).validateOriginPolicy(); err != nil {
		t.Fatalf("validateOriginPolicy(valid) error = %v", err)
	}
}

func TestWebSocketServerDefaultsAreProductionBounded(t *testing.T) {
	if got := resolveWebSocketReadTimeout(0); got != defaultWebSocketReadTimeout {
		t.Fatalf("read timeout = %v, want %v", got, defaultWebSocketReadTimeout)
	}
	if got := resolveWebSocketIdleTimeout(0); got != defaultWebSocketIdleTimeout {
		t.Fatalf("idle timeout = %v, want %v", got, defaultWebSocketIdleTimeout)
	}
	if got := resolveWebSocketMaxHeaderBytes(0); got != defaultWebSocketMaxHeaderSize {
		t.Fatalf("max header bytes = %d, want %d", got, defaultWebSocketMaxHeaderSize)
	}
	if got := resolveWebSocketReadTimeout(2 * time.Second); got != 2*time.Second {
		t.Fatalf("custom read timeout = %v, want 2s", got)
	}
}
