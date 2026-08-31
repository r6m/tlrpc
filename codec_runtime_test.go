package tlrpc

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
)

func TestDecodeTLObject_MsgsAck(t *testing.T) {
	decoder := newDispatcher()
	decoder.RegisterConstructor(mtprototl.MsgsAckID, func() TLObject { return &mtprototl.MsgsAck{} })
	ack := &mtprototl.MsgsAck{MsgIDs: []int64{11, 22}}
	buf := &bytes.Buffer{}
	if err := ack.SerializeTL(buf); err != nil {
		t.Fatalf("serialize ack: %v", err)
	}
	obj, _, err := decodeTLObject(decoder, buf.Bytes())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	decoded, ok := obj.(*mtprototl.MsgsAck)
	if !ok {
		t.Fatalf("decoded type: %T", obj)
	}
	if len(decoded.MsgIDs) != 2 || decoded.MsgIDs[1] != 22 {
		t.Fatalf("decoded ids: %+v", decoded.MsgIDs)
	}
}

type largeTLObject struct {
	chunk  []byte
	writes int
}

func (*largeTLObject) ConstructorID() uint32         { return 0x01020304 }
func (*largeTLObject) Method() string                { return "" }
func (*largeTLObject) TLName() string                { return "large" }
func (*largeTLObject) DeserializeTL(io.Reader) error { return nil }
func (o *largeTLObject) SerializeTL(w io.Writer) error {
	for range o.writes {
		if _, err := w.Write(o.chunk); err != nil {
			return err
		}
	}
	return nil
}

func TestEncodeTLObjectRejectsBeforeGrowingPastBudget(t *testing.T) {
	object := &largeTLObject{chunk: make([]byte, 1024), writes: 1024}
	_, err := encodeTLObjectWithLimits(object, EncodeLimits{MaxEncodedBytes: 4096})
	if !errors.Is(err, ErrEncodedTLTooLarge) {
		t.Fatalf("encode error = %v, want ErrEncodedTLTooLarge", err)
	}
	allocations := testing.AllocsPerRun(100, func() {
		_, _ = encodeTLObjectWithLimits(object, EncodeLimits{MaxEncodedBytes: 4096})
	})
	if allocations > 8 {
		t.Fatalf("allocations = %.1f, want at most 8", allocations)
	}
}

func TestDecodeTLObjectRejectsBytesBeforeConstructorAllocation(t *testing.T) {
	decoder := newDispatcher()
	decoder.RegisterConstructor(0x01020304, func() TLObject { return &largeTLObject{} })
	data := make([]byte, 65)
	data[0] = 4
	data[1] = 3
	data[2] = 2
	data[3] = 1
	if _, _, err := decodeTLObjectWithLimits(decoder, data, mtproto.DecodeLimits{MaxDecodedBytes: 64}); !errors.Is(err, mtproto.ErrDecodedBytesLimit) {
		t.Fatalf("decode error = %v, want ErrDecodedBytesLimit", err)
	}
}

func TestDecodeTLObjectSerializesConcurrentChildrenSharingBudget(t *testing.T) {
	decoder := newDispatcher()
	decoder.RegisterConstructor(mtprototl.MsgsAckID, func() TLObject { return &mtprototl.MsgsAck{} })
	var encoded bytes.Buffer
	if err := (&mtprototl.MsgsAck{MsgIDs: []int64{11, 22}}).SerializeTL(&encoded); err != nil {
		t.Fatal(err)
	}
	budget, err := mtproto.NewDecodeBudget(mtproto.DecodeLimits{
		MaxDecodedBytes: 1 << 20, MaxVectorElements: 128, MaxObjectNodes: 128,
	})
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errs := make(chan error, 32)
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _, decodeErr := decodeTLObjectWithBudget(decoder, encoded.Bytes(), budget)
			errs <- decodeErr
		}()
	}
	wait.Wait()
	close(errs)
	for decodeErr := range errs {
		if decodeErr != nil {
			t.Fatalf("concurrent child decode: %v", decodeErr)
		}
	}
}
