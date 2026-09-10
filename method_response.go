package tlrpc

import (
	"fmt"

	"github.com/r6m/tlrpc/mtproto"
)

// EncodeTypedResponse validates an interceptor's result against the generated
// method result type, then encodes it once with the effective layer and output
// budget.
func EncodeTypedResponse[T any](response any, layer int, limits EncodeLimits, encode func(*mtproto.Encoder, T) error) ([]byte, error) {
	value, ok := response.(T)
	if !ok {
		return nil, fmt.Errorf("tlrpc: response %T does not satisfy the declared method result", response)
	}
	maxBytes := limits.MaxEncodedBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxEncodedTLBytes
	}
	encoder, err := mtproto.NewBufferEncoder(layer, maxBytes)
	if err != nil {
		return nil, err
	}
	if encode == nil {
		return nil, fmt.Errorf("tlrpc: nil typed response encoder")
	}
	if err := encode(encoder, value); err != nil {
		return nil, err
	}
	return encoder.Bytes(), nil
}
