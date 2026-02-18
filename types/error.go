package types

import (
	"fmt"
	"io"

	"github.com/r6m/tlrpc/mtproto"
)

// Error represents an error in MTProto
type Error struct {
	Code int32
	Text string
}

func (v *Error) ConstructorID() uint32 { return 0xc4b9f9bb }
func (v *Error) Method() string        { return "" }
func (v *Error) TLName() string        { return "error" }

func (v *Error) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, v.ConstructorID()); err != nil {
		return err
	}
	if err := mtproto.WriteInt32(w, v.Code); err != nil {
		return err
	}
	return mtproto.WriteString(w, v.Text)
}

func (v *Error) DeserializeTL(r io.Reader) error {
	ctorID, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctorID != v.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %x, want %x", ctorID, v.ConstructorID())
	}

	code, err := mtproto.ReadInt32(r)
	if err != nil {
		return err
	}
	v.Code = code

	text, err := mtproto.ReadString(r)
	if err != nil {
		return err
	}
	v.Text = text

	return nil
}

// Null represents a null value in MTProto
type Null struct{}

func (v *Null) ConstructorID() uint32 { return 0x56730bcc }
func (v *Null) Method() string        { return "" }
func (v *Null) TLName() string        { return "null" }

func (v *Null) SerializeTL(w io.Writer) error {
	return mtproto.WriteUint32(w, v.ConstructorID())
}

func (v *Null) DeserializeTL(r io.Reader) error {
	ctorID, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctorID != v.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %x, want %x", ctorID, v.ConstructorID())
	}
	return nil
}
