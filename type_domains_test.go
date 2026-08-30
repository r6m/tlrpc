package tlrpc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyConnectionPipelineIsRemoved(t *testing.T) {
	for _, name := range []string{"conn.go", "conn_send.go", "conn_reliability.go", "handshake.go"} {
		if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("legacy runtime file %s still exists or cannot be checked: %v", name, err)
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
