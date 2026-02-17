package tlrpc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoMTProtoMagicIDsInConn(t *testing.T) {
	data, err := os.ReadFile("conn.go")
	if err != nil {
		t.Fatalf("read conn.go: %v", err)
	}
	for _, id := range []string{
		"0x73f1f8dc",
		"0x62d6b459",
		"0x3072cfa1",
		"0xf35c6d01",
		"0x2144ca19",
		"0x7d861a08",
		"0xda69fb52",
		"0x04deb57d",
	} {
		if strings.Contains(string(data), id) {
			t.Fatalf("conn.go contains MTProto constructor ID %s", id)
		}
	}
}

func TestMTProtoTLPackageExists(t *testing.T) {
	required := []string{
		"ids.go",
		"container.go",
		"rpc.go",
		"ack.go",
		"state.go",
		"gzip.go",
	}
	for _, name := range required {
		path := filepath.Join("mtproto", "tl", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
	}
}
