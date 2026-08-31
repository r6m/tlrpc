package mtproto

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
)

const (
	MaxDecodedPayloadBytes       = 1 << 24
	DefaultMaxGzipExpansionRatio = 128
	DefaultMaxGzipWorkBytes      = 32 << 20
)

var (
	ErrDecodedPayloadTooLarge = errors.New("mtproto: decoded payload exceeds limit")
	ErrGzipExpansionRatio     = errors.New("mtproto: gzip expansion ratio exceeds limit")
	ErrGzipWorkLimit          = errors.New("mtproto: gzip decompression work budget exceeded")
)

// DecompressGzip decodes gzip_packed data with a configured and framework-hard
// output ceiling. The extra byte distinguishes an output exactly at the limit
// from one that would continue expanding.
func DecompressGzip(packed []byte, maxOutputBytes int) ([]byte, error) {
	if maxOutputBytes < 0 {
		return nil, fmt.Errorf("mtproto: gzip output limit must not be negative")
	}
	limit := int64(MaxDecodedPayloadBytes)
	if maxOutputBytes > 0 && int64(maxOutputBytes) < limit {
		limit = int64(maxOutputBytes)
	}
	budget, err := NewDecodeBudget(DecodeLimits{
		MaxDecodedBytes:  limit,
		MaxGzipWorkBytes: limit + int64(len(packed)),
	})
	if err != nil {
		return nil, err
	}
	return DecompressGzipWithBudget(packed, budget)
}

// DecompressGzipWithBudget expands one gzip member while charging output and
// work to the shared logical decode budget. Reusing budget across nested
// wrappers prevents each layer from receiving a fresh allowance.
func DecompressGzipWithBudget(packed []byte, budget *DecodeBudget) ([]byte, error) {
	if budget == nil {
		var err error
		budget, err = NewDecodeBudget(DecodeLimits{})
		if err != nil {
			return nil, err
		}

	}
	gr, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gr.Close() }()

	packedBytes := int64(len(packed))
	budget.mu.Lock()
	decodedRemaining := budget.limits.MaxDecodedBytes - budget.decodedBytes
	workRemaining := budget.limits.MaxGzipWorkBytes - budget.gzipWorkBytes
	ratioLimit := saturatingMultiply(packedBytes, budget.limits.MaxGzipRatio)
	budget.mu.Unlock()
	if packedBytes > workRemaining {
		return nil, ErrGzipWorkLimit
	}
	outputWorkRemaining := workRemaining - packedBytes
	limit := minInt64(decodedRemaining, outputWorkRemaining, ratioLimit)
	if limit < 0 {
		limit = 0
	}
	decoded, err := io.ReadAll(io.LimitReader(gr, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(decoded)) > ratioLimit {
		return nil, ErrGzipExpansionRatio
	}
	if int64(len(decoded)) > decodedRemaining {
		return nil, ErrDecodedPayloadTooLarge
	}
	workCharge := packedBytes + int64(len(decoded))
	if workCharge > workRemaining {
		return nil, ErrGzipWorkLimit
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if int64(len(decoded)) > budget.limits.MaxDecodedBytes-budget.decodedBytes {
		return nil, ErrDecodedPayloadTooLarge
	}
	if workCharge > budget.limits.MaxGzipWorkBytes-budget.gzipWorkBytes {
		return nil, ErrGzipWorkLimit
	}
	budget.decodedBytes += int64(len(decoded))
	budget.gzipWorkBytes += workCharge
	return decoded, nil
}

func minInt64(values ...int64) int64 {
	minimum := values[0]
	for _, value := range values[1:] {
		if value < minimum {
			minimum = value
		}
	}
	return minimum
}

func saturatingMultiply(left, right int64) int64 {
	if left == 0 || right == 0 {
		return 0
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	if left > maxInt64/right {
		return maxInt64
	}
	return left * right
}
