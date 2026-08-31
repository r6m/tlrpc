package mtproto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"sync"
)

var (
	ErrInvalidBool         = errors.New("mtproto: invalid bool value")
	ErrStringTooLong       = errors.New("mtproto: string too long")
	ErrVectorTooLong       = errors.New("mtproto: vector element count exceeds limit")
	ErrDecodedBytesLimit   = errors.New("mtproto: aggregate decoded byte budget exceeded")
	ErrWrapperCountLimit   = errors.New("mtproto: wrapper budget exceeded")
	ErrContainerCountLimit = errors.New("mtproto: container budget exceeded")
	ErrVectorCountLimit    = errors.New("mtproto: aggregate vector element budget exceeded")
	ErrObjectNodeLimit     = errors.New("mtproto: aggregate object node budget exceeded")
	ErrObjectDepthLimit    = errors.New("mtproto: object depth budget exceeded")
)

const (
	MaxVectorElements = 1 << 20

	DefaultMaxDecodedBytes   = 16 << 20
	DefaultMaxWrappers       = 16
	DefaultMaxContainers     = 64
	DefaultMaxVectorElements = 1 << 16
	DefaultMaxObjectNodes    = 1 << 18
	DefaultMaxObjectDepth    = 128
)

// DecodeLimits bounds independent dimensions of one logical TL decode. A
// zero-valued field selects its safe framework default. Negative values are
// invalid and are rejected by NewDecodeBudget.
type DecodeLimits struct {
	MaxDecodedBytes   int64
	MaxWrappers       int
	MaxContainers     int
	MaxVectorElements int64
	MaxObjectNodes    int64
	MaxObjectDepth    int
	MaxGzipRatio      int64
	MaxGzipWorkBytes  int64
}

// DecodeBudget is shared by every reader and nested object participating in
// one logical decode. Counter access is synchronized, while LockDecode lets
// callers serialize complete sibling decodes so object depth remains
// path-accurate when container children dispatch concurrently.
type DecodeBudget struct {
	operationMu sync.Mutex
	mu          sync.Mutex
	limits      DecodeLimits

	decodedBytes   int64
	wrapperCount   int
	containerCount int
	vectorElements int64
	objectNodes    int64
	objectDepth    int
	gzipWorkBytes  int64
}

// LockDecode serializes one complete decode operation using this budget.
func (b *DecodeBudget) LockDecode() func() {
	if b == nil {
		return func() {}
	}
	b.operationMu.Lock()
	return b.operationMu.Unlock
}

// NewDecodeBudget validates limits and applies safe framework defaults.
func NewDecodeBudget(limits DecodeLimits) (*DecodeBudget, error) {
	normalized, err := normalizeDecodeLimits(limits)
	if err != nil {
		return nil, err
	}
	return &DecodeBudget{limits: normalized}, nil
}

func normalizeDecodeLimits(limits DecodeLimits) (DecodeLimits, error) {
	if limits.MaxDecodedBytes < 0 || limits.MaxWrappers < 0 || limits.MaxContainers < 0 ||
		limits.MaxVectorElements < 0 || limits.MaxObjectNodes < 0 || limits.MaxObjectDepth < 0 ||
		limits.MaxGzipRatio < 0 || limits.MaxGzipWorkBytes < 0 {
		return DecodeLimits{}, errors.New("mtproto: decode limits must not be negative")
	}
	if limits.MaxDecodedBytes == 0 {
		limits.MaxDecodedBytes = DefaultMaxDecodedBytes
	}
	if limits.MaxWrappers == 0 {
		limits.MaxWrappers = DefaultMaxWrappers
	}
	if limits.MaxContainers == 0 {
		limits.MaxContainers = DefaultMaxContainers
	}
	if limits.MaxVectorElements == 0 {
		limits.MaxVectorElements = DefaultMaxVectorElements
	}
	if limits.MaxObjectNodes == 0 {
		limits.MaxObjectNodes = DefaultMaxObjectNodes
	}
	if limits.MaxObjectDepth == 0 {
		limits.MaxObjectDepth = DefaultMaxObjectDepth
	}
	if limits.MaxGzipRatio == 0 {
		limits.MaxGzipRatio = DefaultMaxGzipExpansionRatio
	}
	if limits.MaxGzipWorkBytes == 0 {
		limits.MaxGzipWorkBytes = DefaultMaxGzipWorkBytes
	}
	return limits, nil
}

// BudgetReader wraps r so all bytes read count against budget. Nested readers
// should be derived with PrependReader to preserve the same budget.
type BudgetReader struct {
	reader io.Reader
	budget *DecodeBudget
}

func NewBudgetReader(r io.Reader, budget *DecodeBudget) *BudgetReader {
	if budget == nil {
		budget, _ = NewDecodeBudget(DecodeLimits{})
	}
	return &BudgetReader{reader: r, budget: budget}
}

func (r *BudgetReader) Read(p []byte) (int, error) {
	if r == nil || r.reader == nil {
		return 0, io.EOF
	}
	if sized, ok := r.reader.(interface{ Len() int }); ok && sized.Len() == 0 {
		return 0, io.EOF
	}
	if r.budget == nil {
		return r.reader.Read(p)
	}
	r.budget.mu.Lock()
	remaining := r.budget.limits.MaxDecodedBytes - r.budget.decodedBytes
	if remaining <= 0 {
		r.budget.mu.Unlock()
		return 0, ErrDecodedBytesLimit
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	reserved := int64(len(p))
	r.budget.decodedBytes += reserved
	r.budget.mu.Unlock()
	n, err := r.reader.Read(p)
	r.budget.mu.Lock()
	r.budget.decodedBytes -= reserved - int64(n)
	r.budget.mu.Unlock()
	return n, err
}

func (r *BudgetReader) DecodeBudget() *DecodeBudget { return r.budget }

// Len preserves bounded-reader allocation checks without exposing bytes.Reader.
func (r *BudgetReader) Len() int {
	if r == nil || r.budget == nil {
		if r != nil {
			if sized, ok := r.reader.(interface{ Len() int }); ok {
				return sized.Len()
			}
		}
		return 0
	}
	sizedLength := -1
	if sized, ok := r.reader.(interface{ Len() int }); ok {
		sizedLength = sized.Len()
	}
	r.budget.mu.Lock()
	remaining := r.budget.limits.MaxDecodedBytes - r.budget.decodedBytes
	r.budget.mu.Unlock()
	if remaining < 0 {
		remaining = 0
	}
	if sizedLength >= 0 && int64(sizedLength) < remaining {
		return sizedLength
	}
	return int(remaining)
}

type budgetProvider interface {
	DecodeBudget() *DecodeBudget
}

func budgetFromReader(r io.Reader) *DecodeBudget {
	if provider, ok := r.(budgetProvider); ok {
		return provider.DecodeBudget()
	}
	return nil
}

// EnterObject accounts one generated TL object and its nesting depth. The
// returned leave function must be deferred by generated DeserializeTL methods.
func EnterObject(r io.Reader) (func(), error) {
	budget := budgetFromReader(r)
	if budget == nil {
		return func() {}, nil
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.objectNodes >= budget.limits.MaxObjectNodes {
		return nil, ErrObjectNodeLimit
	}
	if budget.objectDepth >= budget.limits.MaxObjectDepth {
		return nil, ErrObjectDepthLimit
	}
	budget.objectNodes++
	budget.objectDepth++
	return func() {
		budget.mu.Lock()
		budget.objectDepth--
		budget.mu.Unlock()
	}, nil
}

func ConsumeWrapper(r io.Reader) error {
	budget := budgetFromReader(r)
	if budget == nil {
		return nil
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.wrapperCount >= budget.limits.MaxWrappers {
		return ErrWrapperCountLimit
	}
	budget.wrapperCount++
	return nil
}

func ConsumeContainer(r io.Reader) error {
	budget := budgetFromReader(r)
	if budget == nil {
		return nil
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.containerCount >= budget.limits.MaxContainers {
		return ErrContainerCountLimit
	}
	budget.containerCount++
	return nil
}

func consumeVectorElements(r io.Reader, count int32) error {
	budget := budgetFromReader(r)
	if budget == nil {
		return nil
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if int64(count) > budget.limits.MaxVectorElements-budget.vectorElements {
		return ErrVectorCountLimit
	}
	budget.vectorElements += int64(count)
	return nil
}

type prependedBudgetReader struct {
	prefix *bytes.Reader
	reader io.Reader
	budget *DecodeBudget
}

func (r *prependedBudgetReader) Read(p []byte) (int, error) {
	if r.prefix.Len() > 0 {
		return r.prefix.Read(p)
	}
	return r.reader.Read(p)
}

func (r *prependedBudgetReader) DecodeBudget() *DecodeBudget { return r.budget }

func (r *prependedBudgetReader) Len() int {
	length := r.prefix.Len()
	if sized, ok := r.reader.(interface{ Len() int }); ok {
		length += sized.Len()
	}
	return length
}

// PrependReader replays already-consumed constructor bytes without charging
// them twice while preserving the parent's aggregate decode budget.
func PrependReader(prefix []byte, r io.Reader) io.Reader {
	return &prependedBudgetReader{
		prefix: bytes.NewReader(append([]byte(nil), prefix...)),
		reader: r,
		budget: budgetFromReader(r),
	}
}

// ReadInt32 reads an int32 in little-endian.
func ReadInt32(r io.Reader) (int32, error) {
	v, err := ReadUint32(r)
	return int32(v), err
}

// ReadUint32 reads a uint32 in little-endian.
func WriteBigInt(w io.Writer, n *big.Int, size int) error {
	data := n.Bytes()
	if len(data) < size {
		padded := make([]byte, size)
		copy(padded[size-len(data):], data)
		_, err := w.Write(padded)
		return err
	}
	_, err := w.Write(data[len(data)-size:])
	return err
}

func ReadUint32(r io.Reader) (uint32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf[:]), nil
}

// ReadInt64 reads an int64 in little-endian.
func ReadInt64(r io.Reader) (int64, error) {
	v, err := ReadUint64(r)
	return int64(v), err
}

// ReadUint64 reads a uint64 in little-endian.
func ReadUint64(r io.Reader) (uint64, error) {
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(buf[:]), nil
}

// ReadInt128 reads a 16-byte integer.
func ReadInt128(r io.Reader) ([16]byte, error) {
	var buf [16]byte
	_, err := io.ReadFull(r, buf[:])
	return buf, err
}

// ReadInt256 reads a 32-byte integer.
func ReadInt256(r io.Reader) ([32]byte, error) {
	var buf [32]byte
	_, err := io.ReadFull(r, buf[:])
	return buf, err
}

// ReadDouble reads a float64 in little-endian IEEE 754.
func ReadDouble(r io.Reader) (float64, error) {
	v, err := ReadUint64(r)
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(v), nil
}

// ReadString reads a TL string.
func ReadString(r io.Reader) (string, error) {
	b, err := ReadBytes(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ReadBytes reads TL bytes with length prefix and padding.
func ReadBytes(r io.Reader) ([]byte, error) {
	var sizeBuf [4]byte
	if _, err := io.ReadFull(r, sizeBuf[:1]); err != nil {
		return nil, err
	}
	first := sizeBuf[0]

	var length int
	var headerSize int
	if first < 254 {
		length = int(first)
		headerSize = 1
	} else {
		if _, err := io.ReadFull(r, sizeBuf[:3]); err != nil {
			return nil, err
		}
		sizeBuf[3] = 0
		length = int(binary.LittleEndian.Uint32(sizeBuf[:]))
		headerSize = 4
	}

	if length > math.MaxInt32 {
		return nil, ErrStringTooLong
	}

	data, err := ReadSizedBytes(r, length)
	if err != nil {
		return nil, err
	}

	padding := (4 - ((headerSize + length) % 4)) % 4
	if padding > 0 {
		var pad [3]byte
		if _, err := io.ReadFull(r, pad[:padding]); err != nil {
			return nil, err
		}
	}

	return data, nil
}

// ReadSizedBytes reads exactly length bytes while rejecting a declaration that
// exceeds the known remaining input before allocating it. Runtime decoders use
// bounded in-memory readers, so malformed nested lengths fail at this boundary
// rather than turning an attacker-controlled declaration into an allocation.
func ReadSizedBytes(r io.Reader, length int) ([]byte, error) {
	if length < 0 {
		return nil, ErrInvalidMessageLength
	}
	if remaining, ok := r.(interface{ Len() int }); ok && length > remaining.Len() {
		return nil, io.ErrUnexpectedEOF
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	return data, nil
}

// ReadBool reads a TL Bool.
func ReadBool(r io.Reader) (bool, error) {
	v, err := ReadUint32(r)
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

// ReadVector reads a TL vector under the framework hard element ceiling.
// Protocol-specific decoders should use ReadVectorBounded with a tighter
// semantic limit when one exists.
func ReadVector(r io.Reader, fn func() error) error {
	return ReadVectorBounded(r, MaxVectorElements, fn)
}

// ReadVectorBounded validates the declared element count before invoking fn.
// It prevents an attacker-controlled count from causing unbounded loops or
// aggregate slice growth even when every individual element is small.
func ReadVectorBounded(r io.Reader, maxElements int, fn func() error) error {
	if maxElements < 0 {
		return fmt.Errorf("%w: invalid limit %d", ErrVectorTooLong, maxElements)
	}
	ctor, err := ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != VectorConstructorID {
		return fmt.Errorf("mtproto: invalid vector constructor: %08x", ctor)
	}
	count, err := ReadInt32(r)
	if err != nil {
		return err
	}
	if count < 0 {
		return fmt.Errorf("mtproto: invalid vector length: %d", count)
	}
	if int64(count) > int64(maxElements) {
		return fmt.Errorf("%w: got %d, limit %d", ErrVectorTooLong, count, maxElements)
	}
	if err := consumeVectorElements(r, count); err != nil {
		return err
	}
	for i := int32(0); i < count; i++ {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}

// ReadBareVectorBounded reads a bare vector count followed by its elements.
// MTProto uses this encoding for vector<%Message> in msg_container: unlike a
// boxed TL vector, no 0x1cb5c415 constructor precedes the count.
func ReadBareVectorBounded(r io.Reader, maxElements int, fn func() error) error {
	if maxElements < 0 {
		return fmt.Errorf("%w: invalid limit %d", ErrVectorTooLong, maxElements)
	}
	count, err := ReadInt32(r)
	if err != nil {
		return err
	}
	if count < 0 {
		return fmt.Errorf("mtproto: invalid vector length: %d", count)
	}
	if int64(count) > int64(maxElements) {
		return fmt.Errorf("%w: got %d, limit %d", ErrVectorTooLong, count, maxElements)
	}
	if err := consumeVectorElements(r, count); err != nil {
		return err
	}
	for i := int32(0); i < count; i++ {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}
