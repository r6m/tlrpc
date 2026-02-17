package tl

import (
	"fmt"
	"io"

	"github.com/r6m/tlrpc/mtproto"
)

// GzipPacked corresponds to gzip_packed#3072cfa1 packed_data:bytes = Object;
type GzipPacked struct {
	PackedData []byte
}

func (*GzipPacked) ConstructorID() uint32 { return GzipPackedID }

func (g *GzipPacked) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, g.ConstructorID()); err != nil {
		return err
	}
	return mtproto.WriteBytes(w, g.PackedData)
}

func (g *GzipPacked) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != g.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, g.ConstructorID())
	}
	g.PackedData, err = mtproto.ReadBytes(r)
	return err
}
