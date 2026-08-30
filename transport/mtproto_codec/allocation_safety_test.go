package mtprotocodec

import (
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

// boundedCodec is the final packet-reader contract: callers resolve the
// configured and framework-hard limits into one effective payload ceiling,
// and codecs reject declarations above it before reading payload bytes.
type boundedCodec interface {
	ReadPacket(r io.Reader, maxPayloadBytes int) (payload []byte, quickAck *uint32, err error)
}

var (
	_ boundedCodec = (*Abridged)(nil)
	_ boundedCodec = (*Intermediate)(nil)
	_ boundedCodec = (*PaddedIntermediate)(nil)
	_ boundedCodec = (*Full)(nil)
)

var errUnexpectedPayloadRead = errors.New("codec requested payload after rejecting oversized header")

type headerOnlyReader struct {
	header               []byte
	payloadReadRequested bool
}

func (r *headerOnlyReader) Read(p []byte) (int, error) {
	if len(r.header) != 0 {
		n := copy(p, r.header)
		r.header = r.header[n:]
		return n, nil
	}
	r.payloadReadRequested = true
	return 0, errUnexpectedPayloadRead
}

func TestBoundedCodecsRejectOversizedDeclaredPayloadBeforeRead(t *testing.T) {
	const maxPayloadBytes = 16
	const declaredPayloadBytes = 20

	uint32Header := func(length uint32, headerBytes int) []byte {
		header := make([]byte, headerBytes)
		binary.LittleEndian.PutUint32(header, length)
		return header
	}

	tests := []struct {
		name   string
		codec  boundedCodec
		header []byte
	}{
		{
			name:   "abridged",
			codec:  &Abridged{},
			header: []byte{declaredPayloadBytes / 4},
		},
		{
			name:   "intermediate",
			codec:  &Intermediate{},
			header: uint32Header(declaredPayloadBytes, 4),
		},
		{
			name:   "padded-intermediate",
			codec:  &PaddedIntermediate{},
			header: uint32Header(declaredPayloadBytes, 4),
		},
		{
			name:  "full",
			codec: &Full{},
			// Full's declared length includes its 8-byte header and 4-byte CRC.
			header: uint32Header(declaredPayloadBytes+fullMinLen, 8),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &headerOnlyReader{header: tt.header}

			payload, quickAck, err := tt.codec.ReadPacket(reader, maxPayloadBytes)

			if !errors.Is(err, ErrPayloadTooLarge) {
				t.Fatalf("ReadPacket() error = %v, want %v", err, ErrPayloadTooLarge)
			}
			if payload != nil {
				t.Fatalf("ReadPacket() payload = %d bytes, want nil", len(payload))
			}
			if quickAck != nil {
				t.Fatalf("ReadPacket() quick ack = %d, want nil", *quickAck)
			}
			if reader.payloadReadRequested {
				t.Fatal("ReadPacket() requested payload bytes after oversized header")
			}
			if len(reader.header) != 0 {
				t.Fatalf("ReadPacket() left %d framing-header bytes unread", len(reader.header))
			}
		})
	}
}
