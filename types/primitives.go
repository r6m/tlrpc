package types

import (
	"io"

	"github.com/r6m/tlrpc/mtproto"
)

// String represents a string in MTProto
type String string

func (v *String) ConstructorID() uint32 { return 0xb5286e24 }
func (v *String) Method() string        { return "" }
func (v *String) TLName() string        { return "string" }

func (v *String) SerializeTL(w io.Writer) error {
	return mtproto.WriteString(w, string(*v))
}

func (v *String) DeserializeTL(r io.Reader) error {
	str, err := mtproto.ReadString(r)
	if err != nil {
		return err
	}
	*v = String(str)
	return nil
}

// Bytes represents a byte array in MTProto
type Bytes []byte

func (v *Bytes) ConstructorID() uint32 { return 0x0a1cdbd1 }
func (v *Bytes) Method() string        { return "" }
func (v *Bytes) TLName() string        { return "bytes" }

func (v *Bytes) SerializeTL(w io.Writer) error {
	return mtproto.WriteBytes(w, []byte(*v))
}

func (v *Bytes) DeserializeTL(r io.Reader) error {
	bytes, err := mtproto.ReadBytes(r)
	if err != nil {
		return err
	}
	*v = Bytes(bytes)
	return nil
}

// Int128 represents a 128-bit integer in MTProto
type Int128 [16]byte

func (v *Int128) ConstructorID() uint32 { return 0x84c1e679 }
func (v *Int128) Method() string        { return "" }
func (v *Int128) TLName() string        { return "int128" }

func (v *Int128) SerializeTL(w io.Writer) error {
	return mtproto.WriteInt128(w, *v)
}

func (v *Int128) DeserializeTL(r io.Reader) error {
	bytes, err := mtproto.ReadInt128(r)
	if err != nil {
		return err
	}
	*v = Int128(bytes)
	return nil
}

// Int256 represents a 256-bit integer in MTProto
type Int256 [32]byte

func (v *Int256) ConstructorID() uint32 { return 0x7bed4774 }
func (v *Int256) Method() string        { return "" }
func (v *Int256) TLName() string        { return "int256" }

func (v *Int256) SerializeTL(w io.Writer) error {
	return mtproto.WriteInt256(w, *v)
}

func (v *Int256) DeserializeTL(r io.Reader) error {
	bytes, err := mtproto.ReadInt256(r)
	if err != nil {
		return err
	}
	*v = Int256(bytes)
	return nil
}

// Double represents a double precision float in MTProto
type Double float64

func (v *Double) ConstructorID() uint32 { return 0x2210c154 }
func (v *Double) Method() string        { return "" }
func (v *Double) TLName() string        { return "double" }

func (v *Double) SerializeTL(w io.Writer) error {
	return mtproto.WriteDouble(w, float64(*v))
}

func (v *Double) DeserializeTL(r io.Reader) error {
	val, err := mtproto.ReadDouble(r)
	if err != nil {
		return err
	}
	*v = Double(val)
	return nil
}