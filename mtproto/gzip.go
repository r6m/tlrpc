package mtproto

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
)

const MaxDecodedPayloadBytes = 1 << 24

var ErrDecodedPayloadTooLarge = errors.New("mtproto: decoded payload exceeds limit")

// DecompressGzip decodes gzip_packed data with a configured and framework-hard
// output ceiling. The extra byte distinguishes an output exactly at the limit
// from one that would continue expanding.
func DecompressGzip(packed []byte, maxOutputBytes int) ([]byte, error) {
	limit := MaxDecodedPayloadBytes
	if maxOutputBytes > 0 && maxOutputBytes < limit {
		limit = maxOutputBytes
	}
	gr, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gr.Close() }()

	decoded, err := io.ReadAll(io.LimitReader(gr, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(decoded) > limit {
		return nil, ErrDecodedPayloadTooLarge
	}
	return decoded, nil
}
