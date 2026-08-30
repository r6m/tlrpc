package mtproto

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestReadBoolInvalid(t *testing.T) {
	buf := &bytes.Buffer{}
	if err := WriteUint32(buf, 0xdeadbeef); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ReadBool(buf); err != ErrInvalidBool {
		t.Fatalf("expected invalid bool error")
	}
}

func TestReadBytesUnexpectedEOF(t *testing.T) {
	buf := bytes.NewBuffer([]byte{0x04, 0x01})
	if _, err := ReadBytes(buf); err == nil {
		t.Fatalf("expected EOF")
	}
}

func TestNestedTLBytesDecodingSafety(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{
			name:  "short length prefix",
			input: []byte{64, 0xaa},
		},
		{
			name:  "long length prefix",
			input: []byte{0xfe, 64, 0, 0, 0xaa},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := newGuardedReader(tt.input, 16)
			if _, err := ReadBytes(reader); err == nil {
				t.Fatal("ReadBytes accepted a length larger than the remaining input")
			}
			if reader.oversizedRead {
				t.Errorf("ReadBytes requested more than %d bytes in one payload read", reader.maxRead)
			}
		})
	}
}

var errOversizedTestRead = errors.New("test reader: oversized read request")

type guardedReader struct {
	reader        *bytes.Reader
	maxRead       int
	oversizedRead bool
}

func newGuardedReader(data []byte, maxRead int) *guardedReader {
	return &guardedReader{reader: bytes.NewReader(data), maxRead: maxRead}
}

func (r *guardedReader) Read(p []byte) (int, error) {
	if len(p) > r.maxRead {
		r.oversizedRead = true
		return 0, errOversizedTestRead
	}
	return r.reader.Read(p)
}

func (r *guardedReader) Len() int {
	return r.reader.Len()
}

var _ io.Reader = (*guardedReader)(nil)

func TestReadVectorBoundedRejectsCountBeforeDecodingElements(t *testing.T) {
	var buffer bytes.Buffer
	if err := WriteUint32(&buffer, VectorConstructorID); err != nil {
		t.Fatal(err)
	}
	if err := WriteInt32(&buffer, 3); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err := ReadVectorBounded(&buffer, 2, func() error {
		calls++
		return nil
	})
	if !errors.Is(err, ErrVectorTooLong) {
		t.Fatalf("ReadVectorBounded error = %v, want ErrVectorTooLong", err)
	}
	if calls != 0 {
		t.Fatalf("element decoder calls = %d, want 0", calls)
	}
}
