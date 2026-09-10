package mtproto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"testing"
)

func TestCursorWireAndOwnedValues(t *testing.T) {
	e, err := NewBufferEncoder(229, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.WriteInt32(-7); err != nil {
		t.Fatal(err)
	}
	if err := e.WriteInt64(-1 << 40); err != nil {
		t.Fatal(err)
	}
	if err := e.WriteDouble(math.Pi); err != nil {
		t.Fatal(err)
	}
	if err := e.WriteString("owned text"); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte{42}, 300)
	if err := e.WriteBytes(payload); err != nil {
		t.Fatal(err)
	}
	if err := e.WriteVectorHeader(2); err != nil {
		t.Fatal(err)
	}
	if err := e.WriteBool(true); err != nil {
		t.Fatal(err)
	}
	if err := e.WriteBool(false); err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	WriteInt32(&want, -7)
	WriteInt64(&want, -1<<40)
	WriteDouble(&want, math.Pi)
	WriteString(&want, "owned text")
	WriteBytes(&want, payload)
	WriteVectorHeader(&want, 2)
	WriteBool(&want, true)
	WriteBool(&want, false)
	if !bytes.Equal(e.Bytes(), want.Bytes()) {
		t.Fatalf("wire mismatch %x", e.Bytes())
	}
	for _, stream := range []bool{false, true} {
		input := bytes.Clone(e.Bytes())
		d := NewDecoderBytes(input, 229, nil)
		if stream {
			d = NewDecoder(WithLayerReader(bytes.NewReader(input), 229))
		}
		if NewDecoder(d) != d || d.TLLayer() != 229 {
			t.Fatal("cursor identity/layer lost")
		}
		if v, err := d.ReadInt32(); err != nil || v != -7 {
			t.Fatal(v, err)
		}
		if v, err := d.ReadInt64(); err != nil || v != -1<<40 {
			t.Fatal(v, err)
		}
		if v, err := d.ReadDouble(); err != nil || v != math.Pi {
			t.Fatal(v, err)
		}
		text, err := d.ReadString()
		if err != nil {
			t.Fatal(err)
		}
		got, err := d.ReadBytes()
		if err != nil {
			t.Fatal(err)
		}
		if count, err := d.ReadVectorCount(2, 4); err != nil || count != 2 {
			t.Fatal(count, err)
		}
		if v, err := d.ReadBool(); err != nil || !v {
			t.Fatal(v, err)
		}
		if v, err := d.ReadBool(); err != nil || v {
			t.Fatal(v, err)
		}
		if d.Len() != 0 {
			t.Fatal("trailing bytes", d.Len())
		}
		clear(input)
		if text != "owned text" || !bytes.Equal(got, payload) {
			t.Fatal("decoded service values alias input")
		}
	}
}

func TestCursorLimitsBeforeAllocationAndBalancedObjects(t *testing.T) {
	e, _ := NewBufferEncoder(228, 7)
	if err := e.WriteString("1234"); !errors.Is(err, ErrEncodedBytesLimit) {
		t.Fatal(err)
	}
	if len(e.Bytes()) != 0 {
		t.Fatal("oversized byte field partially emitted")
	}
	if _, err := NewBufferEncoder(0, -1); err == nil {
		t.Fatal("negative budget accepted")
	}
	b, _ := NewDecodeBudget(DecodeLimits{MaxDecodedBytes: 4, MaxObjectDepth: 1, MaxObjectNodes: 2, MaxVectorElements: 2})
	d := NewDecoderBytes(make([]byte, 8), 228, b)
	if err := d.EnterObject(); err != nil {
		t.Fatal(err)
	}
	if err := d.EnterObject(); !errors.Is(err, ErrObjectDepthLimit) {
		t.Fatal(err)
	}
	d.LeaveObject()
	if err := d.EnterObject(); err != nil {
		t.Fatal(err)
	}
	d.LeaveObject()
	if err := d.EnterObject(); !errors.Is(err, ErrObjectNodeLimit) {
		t.Fatal(err)
	}
	if _, err := d.ReadUint32(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ReadUint32(); !errors.Is(err, ErrDecodedBytesLimit) {
		t.Fatal(err)
	}
	vector := binary.LittleEndian.AppendUint32(nil, VectorConstructorID)
	vector = binary.LittleEndian.AppendUint32(vector, 3)
	d = NewDecoderBytes(vector, 228, b)
	if _, err := d.ReadVectorCount(2, 4); err == nil {
		t.Fatal("oversized vector accepted")
	}
	d = NewDecoderBytes(vector, 228, nil)
	if _, err := d.ReadVectorCount(10, 4); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
}

func TestTLBytesLengthBoundaries(t *testing.T) {
	tooLarge := make([]byte, MaxTLBytesLength+1)
	if err := WriteBytes(io.Discard, tooLarge); !errors.Is(err, ErrStringTooLong) {
		t.Fatal(err)
	}
	e, _ := NewBufferEncoder(0, len(tooLarge)+8)
	if err := e.WriteBytes(tooLarge); !errors.Is(err, ErrStringTooLong) {
		t.Fatal(err)
	}
	if len(e.Bytes()) != 0 {
		t.Fatal("invalid length emitted bytes")
	}
	if err := WriteBytes(io.Discard, tooLarge[:MaxTLBytesLength]); err != nil {
		t.Fatal(err)
	}
	for _, payload := range [][]byte{{255, 0, 0, 0}, {254, 255, 255, 255}, {3, 1, 2}} {
		d := NewDecoderBytes(payload, 0, nil)
		if _, err := d.ReadBytes(); err == nil {
			t.Fatalf("accepted malformed bytes %x", payload)
		}
		if _, err := ReadBytes(bytes.NewReader(payload)); err == nil {
			t.Fatalf("stream accepted malformed bytes %x", payload)
		}
	}
}

type shortCursorWriter struct{}

func FuzzCursorBytesParity(f *testing.F) {
	for _, seed := range [][]byte{{}, {0, 0, 0, 0}, {2, 1, 2, 0}, {255, 0, 0, 0}, {254, 255, 255, 255}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 1024 {
			t.Skip()
		}
		referenceInput := bytes.NewReader(input)
		budget, _ := NewDecodeBudget(DecodeLimits{MaxDecodedBytes: 1024})
		want, wantErr := ReadBytes(NewBudgetReader(referenceInput, budget))
		for _, buffered := range []bool{false, true} {
			budget, _ := NewDecodeBudget(DecodeLimits{MaxDecodedBytes: 1024})
			var d *Decoder
			if buffered {
				d = NewDecoderBytes(input, 0, budget)
			} else {
				d = NewDecoder(NewBudgetReader(bytes.NewReader(input), budget))
			}
			got, err := d.ReadBytes()
			if (err == nil) != (wantErr == nil) || err == nil && (!bytes.Equal(got, want) || d.Len() != referenceInput.Len()) {
				t.Fatalf("buffered=%t input=%x: got %x/%v, want %x/%v", buffered, input, got, err, want, wantErr)
			}
		}
	})
}

func TestCursorTrailingBytesRemainVisibleAfterBudgetExhaustion(t *testing.T) {
	for _, stream := range []bool{false, true} {
		budget, err := NewDecodeBudget(DecodeLimits{MaxDecodedBytes: 4})
		if err != nil {
			t.Fatal(err)
		}
		d := NewDecoderBytes(make([]byte, 8), 0, budget)
		if stream {
			d = NewDecoder(WithLayerReader(NewBudgetReader(bytes.NewReader(make([]byte, 8)), budget), 229))
		}
		if _, err := d.ReadUint32(); err != nil {
			t.Fatal(err)
		}
		if d.Len() != 4 {
			t.Fatalf("trailing bytes hidden by exhausted budget: %d", d.Len())
		}
		if _, err := d.ReadUint32(); !errors.Is(err, ErrDecodedBytesLimit) {
			t.Fatalf("second read = %v", err)
		}
	}
}

func (shortCursorWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }
func TestCursorStreamShortWrite(t *testing.T) {
	e := NewEncoder(shortCursorWriter{})
	if err := e.WriteInt32(7); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
	var output bytes.Buffer
	e = NewEncoder(WithLayerWriter(&output, 229))
	if e.TLLayer() != 229 || NewEncoder(e) != e {
		t.Fatal("encoder context lost")
	}
	if err := e.WriteString("stream"); err != nil {
		t.Fatal(err)
	}
	if value, err := ReadString(&output); err != nil || value != "stream" {
		t.Fatal(value, err)
	}
}
