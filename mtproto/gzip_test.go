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

func TestDecompressGzipWithBudgetEnforcesExpansionRatio(t *testing.T) {
	budget, err := NewDecodeBudget(DecodeLimits{
		MaxDecodedBytes:  4096,
		MaxGzipRatio:     2,
		MaxGzipWorkBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	packed := gzipTestPayload(t, bytes.Repeat([]byte("x"), 1024))
	if _, err := DecompressGzipWithBudget(packed, budget); !errors.Is(err, ErrGzipExpansionRatio) {
		t.Fatalf("ratio error = %v, want ErrGzipExpansionRatio", err)
	}
}

func TestNestedGzipSharesAggregateWorkBudget(t *testing.T) {
	payload := bytes.Repeat([]byte("z"), 256)
	inner := gzipTestPayload(t, payload)
	outer := gzipTestPayload(t, inner)
	budget, err := NewDecodeBudget(DecodeLimits{
		MaxDecodedBytes:  4096,
		MaxGzipRatio:     256,
		MaxGzipWorkBytes: int64(len(inner) + len(payload) - 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	decodedInner, err := DecompressGzipWithBudget(outer, budget)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecompressGzipWithBudget(decodedInner, budget); !errors.Is(err, ErrGzipWorkLimit) {
		t.Fatalf("nested gzip error = %v, want ErrGzipWorkLimit", err)
	}
}

func TestGzipWorkBudgetChargesCompressedInput(t *testing.T) {
	packed := gzipTestPayload(t, []byte("small"))
	budget, err := NewDecodeBudget(DecodeLimits{
		MaxDecodedBytes: 1024, MaxGzipRatio: 128, MaxGzipWorkBytes: int64(len(packed) - 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecompressGzipWithBudget(packed, budget); !errors.Is(err, ErrGzipWorkLimit) {
		t.Fatalf("compressed input work error = %v, want ErrGzipWorkLimit", err)
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
