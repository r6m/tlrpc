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

func TestReadBareVectorBoundedReadsCountWithoutConstructor(t *testing.T) {
	var buffer bytes.Buffer
	if err := WriteInt32(&buffer, 2); err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err := ReadBareVectorBounded(&buffer, 2, func() error {
		calls++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("element decoder calls = %d, want 2", calls)
	}
}

func TestDecodeBudgetBoundsIndependentDimensions(t *testing.T) {
	budget, err := NewDecodeBudget(DecodeLimits{
		MaxDecodedBytes:   8,
		MaxWrappers:       1,
		MaxContainers:     1,
		MaxVectorElements: 2,
		MaxObjectNodes:    2,
		MaxObjectDepth:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	reader := NewBudgetReader(bytes.NewReader(make([]byte, 12)), budget)
	if _, err := io.ReadAll(reader); !errors.Is(err, ErrDecodedBytesLimit) {
		t.Fatalf("decoded byte error = %v, want ErrDecodedBytesLimit", err)
	}
	if err := ConsumeWrapper(reader); err != nil {
		t.Fatal(err)
	}
	if err := ConsumeWrapper(reader); !errors.Is(err, ErrWrapperCountLimit) {
		t.Fatalf("wrapper error = %v", err)
	}
	if err := ConsumeContainer(reader); err != nil {
		t.Fatal(err)
	}
	if err := ConsumeContainer(reader); !errors.Is(err, ErrContainerCountLimit) {
		t.Fatalf("container error = %v", err)
	}
	leave, err := EnterObject(reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EnterObject(reader); !errors.Is(err, ErrObjectDepthLimit) {
		t.Fatalf("depth error = %v", err)
	}
	leave()
	leave, err = EnterObject(reader)
	if err != nil {
		t.Fatal(err)
	}
	leave()
	if _, err := EnterObject(reader); !errors.Is(err, ErrObjectNodeLimit) {
		t.Fatalf("node error = %v", err)
	}
}

func TestDecodeBudgetAggregatesVectorElements(t *testing.T) {
	budget, err := NewDecodeBudget(DecodeLimits{MaxVectorElements: 3})
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	for range 2 {
		if err := WriteVectorHeader(&encoded, 2); err != nil {
			t.Fatal(err)
		}
	}
	reader := NewBudgetReader(bytes.NewReader(encoded.Bytes()), budget)
	if err := ReadVector(reader, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := ReadVector(reader, func() error { return nil }); !errors.Is(err, ErrVectorCountLimit) {
		t.Fatalf("second vector error = %v, want ErrVectorCountLimit", err)
	}
}

func TestPrependReaderPreservesBudgetWithoutDoubleCharging(t *testing.T) {
	budget, err := NewDecodeBudget(DecodeLimits{MaxDecodedBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	parent := NewBudgetReader(bytes.NewReader([]byte{5, 6, 7, 8}), budget)
	reader := PrependReader([]byte{1, 2, 3, 4}, parent)
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Fatalf("read = %v", got)
	}
	if budget.decodedBytes != 4 {
		t.Fatalf("charged bytes = %d, want 4", budget.decodedBytes)
	}
}

func TestReadBytesHugeDeclarationDoesNotAllocatePayload(t *testing.T) {
	input := []byte{0xfe, 0xff, 0xff, 0x7f}
	allocations := testing.AllocsPerRun(100, func() {
		budget, err := NewDecodeBudget(DecodeLimits{MaxDecodedBytes: 64})
		if err != nil {
			panic(err)
		}
		_, _ = ReadBytes(NewBudgetReader(bytes.NewReader(input), budget))
	})
	if allocations > 6 {
		t.Fatalf("allocations = %.1f, want at most 6", allocations)
	}
}
