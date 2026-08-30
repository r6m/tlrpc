package mtproto

import (
	"bytes"
	"compress/gzip"
	"errors"
	"testing"
)

func TestDecompressGzipEnforcesOutputBudget(t *testing.T) {
	packed := gzipTestPayload(t, bytes.Repeat([]byte("a"), 65))

	if _, err := DecompressGzip(packed, 64); !errors.Is(err, ErrDecodedPayloadTooLarge) {
		t.Fatalf("DecompressGzip() error = %v, want ErrDecodedPayloadTooLarge", err)
	}
}

func TestDecompressGzipAllowsOutputAtBudget(t *testing.T) {
	want := bytes.Repeat([]byte("b"), 64)
	got, err := DecompressGzip(gzipTestPayload(t, want), len(want))
	if err != nil {
		t.Fatalf("DecompressGzip(): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("decoded payload differs: got %d bytes", len(got))
	}
}

func gzipTestPayload(t *testing.T, payload []byte) []byte {
	t.Helper()
	var packed bytes.Buffer
	zw := gzip.NewWriter(&packed)
	if _, err := zw.Write(payload); err != nil {
		t.Fatalf("write gzip payload: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return packed.Bytes()
}
