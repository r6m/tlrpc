package types

import (
	"fmt"
	"io"

	"github.com/r6m/tlrpc/mtproto"
)

// BoolFalse represents the false boolean value in MTProto
type BoolFalse struct{}

func (v *BoolFalse) ConstructorID() uint32 { return 0xbc799737 }
func (v *BoolFalse) Method() string        { return "" }
func (v *BoolFalse) TLName() string        { return "boolFalse" }

func (v *BoolFalse) SerializeTL(w io.Writer) error {
	return mtproto.WriteUint32(w, v.ConstructorID())
}

func (v *BoolFalse) DeserializeTL(r io.Reader) error {
	ctorID, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctorID != v.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %x, want %x", ctorID, v.ConstructorID())
	}
	return nil
}

// BoolTrue represents the true boolean value in MTProto
type BoolTrue struct{}

func (v *BoolTrue) ConstructorID() uint32 { return 0x997275b5 }
func (v *BoolTrue) Method() string        { return "" }
func (v *BoolTrue) TLName() string        { return "boolTrue" }

func (v *BoolTrue) SerializeTL(w io.Writer) error {
	return mtproto.WriteUint32(w, v.ConstructorID())
}

func (v *BoolTrue) DeserializeTL(r io.Reader) error {
	ctorID, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctorID != v.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %x, want %x", ctorID, v.ConstructorID())
	}
	return nil
}

// True represents a unit type (void/true) in MTProto
type True struct{}

func (v *True) ConstructorID() uint32 { return 0x3fedd339 }
func (v *True) Method() string        { return "" }
func (v *True) TLName() string        { return "true" }

func (v *True) SerializeTL(w io.Writer) error {
	return mtproto.WriteUint32(w, v.ConstructorID())
}

func (v *True) DeserializeTL(r io.Reader) error {
	ctorID, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctorID != v.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %x, want %x", ctorID, v.ConstructorID())
	}
	return nil
}