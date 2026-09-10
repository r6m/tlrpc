package mtproto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

const MaxTLBytesLength = 1<<24 - 1

var ErrEncodedBytesLimit = errors.New("tlrpc: encoded TL object exceeds limit")

// Encoder is the shared bounded cursor used by generated field codecs. Stream
// entry points adapt once; nested values reuse the same cursor and object budget.
// An Encoder is owned by one encoding operation and is not concurrent-safe.
type Encoder struct {
	writer   io.Writer
	data     []byte
	limit    int
	layer    int
	state    *encodeState
	ownState encodeState
	scratch  [32]byte
}

func NewEncoder(w io.Writer) *Encoder {
	if e, ok := w.(*Encoder); ok {
		return e
	}
	e := &Encoder{writer: w, layer: TLLayer(w), state: encodeStateFromWriter(w)}
	if e.state == nil {
		e.state = &e.ownState
	}
	return e
}

// NewBufferEncoder creates a bounded append encoder. Zero selects the default
// frame-sized limit. Bytes remains owned by the caller; storage is never pooled.
func NewBufferEncoder(layer, maxBytes int) (*Encoder, error) {
	if maxBytes < 0 {
		return nil, fmt.Errorf("mtproto: negative encoded byte limit")
	}
	if maxBytes == 0 {
		maxBytes = DefaultMaxDecodedBytes
	}
	e := &Encoder{layer: layer, limit: maxBytes}
	e.state = &e.ownState
	return e, nil
}
func (e *Encoder) Bytes() []byte             { return e.data }
func (e *Encoder) TLLayer() int              { return e.layer }
func (e *Encoder) encodeState() *encodeState { return e.state }
func (e *Encoder) EnterObject() error {
	if e.state.depth >= DefaultMaxObjectDepth {
		return ErrEncodedObjectDepthLimit
	}
	if e.state.nodes >= DefaultMaxObjectNodes {
		return ErrEncodedObjectNodesLimit
	}
	e.state.depth++
	e.state.nodes++
	return nil
}
func (e *Encoder) LeaveObject() { e.state.depth-- }
func (e *Encoder) reserve(n int) error {
	if n < 0 || n > e.limit-len(e.data) {
		return ErrEncodedBytesLimit
	}
	if n <= cap(e.data)-len(e.data) {
		return nil
	}
	capacity := max(64, cap(e.data)*2)
	capacity = min(e.limit, max(capacity, len(e.data)+n))
	next := make([]byte, len(e.data), capacity)
	copy(next, e.data)
	e.data = next
	return nil
}
func (e *Encoder) Write(p []byte) (int, error) {
	if e.writer != nil {
		n, err := e.writer.Write(p)
		if err == nil && n != len(p) {
			err = io.ErrShortWrite
		}
		return n, err
	}
	if err := e.reserve(len(p)); err != nil {
		return 0, err
	}
	e.data = append(e.data, p...)
	return len(p), nil
}
func (e *Encoder) WriteUint32(v uint32) error {
	if e.writer == nil {
		if err := e.reserve(4); err != nil {
			return err
		}
		e.data = binary.LittleEndian.AppendUint32(e.data, v)
		return nil
	}
	binary.LittleEndian.PutUint32(e.scratch[:4], v)
	_, err := e.Write(e.scratch[:4])
	return err
}
func (e *Encoder) WriteInt32(v int32) error { return e.WriteUint32(uint32(v)) }
func (e *Encoder) WriteUint64(v uint64) error {
	if e.writer == nil {
		if err := e.reserve(8); err != nil {
			return err
		}
		e.data = binary.LittleEndian.AppendUint64(e.data, v)
		return nil
	}
	binary.LittleEndian.PutUint64(e.scratch[:8], v)
	_, err := e.Write(e.scratch[:8])
	return err
}
func (e *Encoder) WriteInt64(v int64) error    { return e.WriteUint64(uint64(v)) }
func (e *Encoder) WriteDouble(v float64) error { return e.WriteUint64(math.Float64bits(v)) }
func (e *Encoder) WriteInt128(v [16]byte) error {
	copy(e.scratch[:16], v[:])
	_, err := e.Write(e.scratch[:16])
	return err
}
func (e *Encoder) WriteInt256(v [32]byte) error {
	copy(e.scratch[:], v[:])
	_, err := e.Write(e.scratch[:])
	return err
}
func (e *Encoder) WriteBool(v bool) error {
	if v {
		return e.WriteUint32(BoolTrue)
	}
	return e.WriteUint32(BoolFalse)
}
func (e *Encoder) bytesHeader(n int) (int, error) {
	if n < 0 || n > MaxTLBytesLength {
		return 0, ErrStringTooLong
	}
	header := 1
	if n < 254 {
		e.scratch[0] = byte(n)
	} else {
		header = 4
		e.scratch[0] = 254
		e.scratch[1] = byte(n)
		e.scratch[2] = byte(n >> 8)
		e.scratch[3] = byte(n >> 16)
	}
	padding := (4 - (header+n)%4) % 4
	if e.writer == nil {
		if err := e.reserve(header + n + padding); err != nil {
			return 0, err
		}
	}
	_, err := e.Write(e.scratch[:header])
	return padding, err
}
func (e *Encoder) padding(n int) error {
	clear(e.scratch[:n])
	_, err := e.Write(e.scratch[:n])
	return err
}
func (e *Encoder) WriteBytes(v []byte) error {
	padding, err := e.bytesHeader(len(v))
	if err != nil {
		return err
	}
	if _, err = e.Write(v); err != nil {
		return err
	}
	return e.padding(padding)
}
func (e *Encoder) WriteString(v string) error {
	padding, err := e.bytesHeader(len(v))
	if err != nil {
		return err
	}
	if e.writer == nil {
		e.data = append(e.data, v...)
	} else {
		n, err := io.WriteString(e.writer, v)
		if err != nil {
			return err
		}
		if n != len(v) {
			return io.ErrShortWrite
		}
	}
	return e.padding(padding)
}
func (e *Encoder) WriteVectorHeader(count int) error {
	if count < 0 || count > math.MaxInt32 {
		return ErrVectorTooLong
	}
	if err := e.WriteUint32(VectorConstructorID); err != nil {
		return err
	}
	return e.WriteInt32(int32(count))
}

// Decoder owns the position in an immutable input buffer or a stream. Generated
// strings and bytes are copied out; its internal spans never escape to services.
// A Decoder is not concurrent-safe. Shared DecodeBudget counters remain guarded.
type Decoder struct {
	reader  io.Reader
	data    []byte
	offset  int
	layer   int
	budget  *DecodeBudget
	scratch [32]byte
}

func NewDecoder(r io.Reader) *Decoder {
	if d, ok := r.(*Decoder); ok {
		return d
	}
	budget := budgetFromReader(r)
	if budget == nil {
		budget, _ = NewDecodeBudget(DecodeLimits{})
		r = NewBudgetReader(r, budget)
	}
	return &Decoder{reader: r, layer: TLLayer(r), budget: budget}
}
func NewDecoderBytes(data []byte, layer int, budget *DecodeBudget) *Decoder {
	if budget == nil {
		budget, _ = NewDecodeBudget(DecodeLimits{})
	}
	return &Decoder{data: data, layer: layer, budget: budget}
}
func (d *Decoder) TLLayer() int                { return d.layer }
func (d *Decoder) DecodeBudget() *DecodeBudget { return d.budget }

// Len reports unread buffer bytes, independently of the remaining decode budget.
// For streams without a known length it reports the remaining byte budget.
func (d *Decoder) Len() int {
	if n, ok := d.physicalLen(); ok {
		return n
	}
	return d.available()
}
func readerPhysicalLen(r io.Reader) (int, bool) {
	switch r := r.(type) {
	case *BudgetReader:
		return readerPhysicalLen(r.reader)
	case *layerReader:
		return readerPhysicalLen(r.Reader)
	case *sizedLayerReader:
		return readerPhysicalLen(r.Reader)
	case *prependedBudgetReader:
		n, ok := readerPhysicalLen(r.reader)
		return r.prefix.Len() + n, ok
	case *Decoder:
		return r.physicalLen()
	case interface{ Len() int }:
		return r.Len(), true
	default:
		return 0, false
	}
}
func (d *Decoder) physicalLen() (int, bool) {
	if d.reader == nil {
		return len(d.data) - d.offset, true
	}
	return readerPhysicalLen(d.reader)
}
func (d *Decoder) available() int {
	d.budget.mu.Lock()
	remaining := d.budget.limits.MaxDecodedBytes - d.budget.decodedBytes
	d.budget.mu.Unlock()
	n := int(max(0, remaining))
	if physical, ok := d.physicalLen(); ok {
		return min(n, physical)
	}
	return n
}
func (d *Decoder) requireAvailable(n int) error {
	if physical, ok := d.physicalLen(); ok && n > physical {
		return io.ErrUnexpectedEOF
	}
	d.budget.mu.Lock()
	defer d.budget.mu.Unlock()
	if int64(n) > d.budget.limits.MaxDecodedBytes-d.budget.decodedBytes {
		return ErrDecodedBytesLimit
	}
	return nil
}
func (d *Decoder) consume(n int) error {
	d.budget.mu.Lock()
	defer d.budget.mu.Unlock()
	if int64(n) > d.budget.limits.MaxDecodedBytes-d.budget.decodedBytes {
		return ErrDecodedBytesLimit
	}
	d.budget.decodedBytes += int64(n)
	return nil
}
func (d *Decoder) take(n int) ([]byte, error) {
	if n < 0 {
		return nil, ErrInvalidMessageLength
	}
	if d.reader == nil {
		if n > len(d.data)-d.offset {
			return nil, io.ErrUnexpectedEOF
		}
		if err := d.consume(n); err != nil {
			return nil, err
		}
		start := d.offset
		d.offset += n
		return d.data[start:d.offset:d.offset], nil
	}
	if err := d.requireAvailable(n); err != nil {
		return nil, err
	}
	var p []byte
	if n <= len(d.scratch) {
		p = d.scratch[:n]
	} else {
		p = make([]byte, n)
	}
	_, err := io.ReadFull(d.reader, p)
	return p, err
}
func (d *Decoder) Read(p []byte) (int, error) {
	if d.reader != nil {
		return d.reader.Read(p)
	}
	if len(p) == 0 {
		return 0, nil
	}
	if d.offset == len(d.data) {
		return 0, io.EOF
	}
	n := min(len(p), len(d.data)-d.offset)
	if err := d.consume(n); err != nil {
		return 0, err
	}
	copy(p, d.data[d.offset:d.offset+n])
	d.offset += n
	return n, nil
}
func (d *Decoder) EnterObject() error {
	b := d.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.objectNodes >= b.limits.MaxObjectNodes {
		return ErrObjectNodeLimit
	}
	if b.objectDepth >= b.limits.MaxObjectDepth {
		return ErrObjectDepthLimit
	}
	b.objectNodes++
	b.objectDepth++
	return nil
}
func (d *Decoder) LeaveObject() { b := d.budget; b.mu.Lock(); b.objectDepth--; b.mu.Unlock() }
func (d *Decoder) ReadUint32() (uint32, error) {
	p, err := d.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(p), nil
}
func (d *Decoder) ReadInt32() (int32, error) { v, err := d.ReadUint32(); return int32(v), err }
func (d *Decoder) ReadUint64() (uint64, error) {
	p, err := d.take(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(p), nil
}
func (d *Decoder) ReadInt64() (int64, error) { v, err := d.ReadUint64(); return int64(v), err }
func (d *Decoder) ReadDouble() (float64, error) {
	v, err := d.ReadUint64()
	return math.Float64frombits(v), err
}
func (d *Decoder) ReadInt128() ([16]byte, error) {
	var v [16]byte
	p, err := d.take(16)
	if err == nil {
		copy(v[:], p)
	}
	return v, err
}
func (d *Decoder) ReadInt256() ([32]byte, error) {
	var v [32]byte
	p, err := d.take(32)
	if err == nil {
		copy(v[:], p)
	}
	return v, err
}
func (d *Decoder) ReadBool() (bool, error) {
	v, err := d.ReadUint32()
	if err != nil {
		return false, err
	}
	if v == BoolTrue {
		return true, nil
	}
	if v == BoolFalse {
		return false, nil
	}
	return false, ErrInvalidBool
}
func (d *Decoder) byteSpan() ([]byte, error) {
	prefix, err := d.take(1)
	if err != nil {
		return nil, err
	}
	first := prefix[0]
	n := int(first)
	header := 1
	if first == 255 {
		return nil, ErrInvalidMessageLength
	}
	if first == 254 {
		p, err := d.take(3)
		if err != nil {
			return nil, err
		}
		n = int(p[0]) | int(p[1])<<8 | int(p[2])<<16
		header = 4
	}
	padding := (4 - (header+n)%4) % 4
	// Reject the full declaration before a stream allocation. For buffer cursors,
	// return a span only to the methods that immediately create an owned value.
	if err := d.requireAvailable(n + padding); err != nil {
		return nil, err
	}
	if d.reader == nil {
		p, err := d.take(n + padding)
		if err != nil {
			return nil, err
		}
		return p[:n:n], nil
	}
	// Padding cannot share scratch storage with a small payload.
	p := make([]byte, n)
	if _, err := io.ReadFull(d.reader, p); err != nil {
		return nil, err
	}
	if _, err := d.take(padding); err != nil {
		return nil, err
	}
	return p, nil
}
func (d *Decoder) ReadBytes() ([]byte, error) {
	p, err := d.byteSpan()
	if err != nil {
		return nil, err
	}
	if d.reader != nil {
		return p, nil
	}
	owned := make([]byte, len(p))
	copy(owned, p)
	return owned, nil
}
func (d *Decoder) ReadString() (string, error) {
	p, err := d.byteSpan()
	if err != nil {
		return "", err
	}
	return string(p), nil
}

// ReadVectorCount validates aggregate counts and a safe minimum wire size before
// generated code reserves a bounded initial slice capacity.
func (d *Decoder) ReadVectorCount(maxElements, minElementBytes int) (int, error) {
	if maxElements < 0 || minElementBytes < 0 {
		return 0, ErrVectorTooLong
	}
	ctor, err := d.ReadUint32()
	if err != nil {
		return 0, err
	}
	if ctor != VectorConstructorID {
		return 0, fmt.Errorf("mtproto: invalid vector constructor: %08x", ctor)
	}
	count, err := d.ReadInt32()
	if err != nil {
		return 0, err
	}
	if count < 0 || int64(count) > int64(maxElements) {
		return 0, ErrVectorTooLong
	}
	if err := consumeVectorElements(d, count); err != nil {
		return 0, err
	}
	if minElementBytes > 0 && int64(count) > int64(d.available()/minElementBytes) {
		return 0, io.ErrUnexpectedEOF
	}
	return int(count), nil
}
