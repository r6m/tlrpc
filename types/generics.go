package types

import (
	"fmt"
	"io"
)

// Vector represents a generic vector type in MTProto
type Vector[T any] struct {
	Items []T
}

func (v *Vector[T]) ConstructorID() uint32 { return 0x1cb5c415 }
func (v *Vector[T]) Method() string        { return "" }
func (v *Vector[T]) TLName() string        { return "vector" }

// Note: Vector serialization is typically handled at the usage site,
// not as a standalone TL object. This is a placeholder implementation.
func (v *Vector[T]) SerializeTL(w io.Writer) error {
	return fmt.Errorf("vector serialization should be handled at usage site")
}

func (v *Vector[T]) DeserializeTL(r io.Reader) error {
	return fmt.Errorf("vector deserialization should be handled at usage site")
}

// Maybe represents an optional value in MTProto
type Maybe[T any] struct {
	Value *T
}

func (v *Maybe[T]) ConstructorID() uint32 {
	if v.Value == nil {
		return 0x56730bcc // null constructor
	}
	// For non-null values, this would need to be handled differently
	// since Maybe is a generic wrapper
	return 0
}

func (v *Maybe[T]) Method() string { return "" }
func (v *Maybe[T]) TLName() string { return "maybe" }

// Note: Maybe serialization is typically handled at the usage site,
// not as a standalone TL object. This is a placeholder implementation.
func (v *Maybe[T]) SerializeTL(w io.Writer) error {
	return fmt.Errorf("maybe serialization should be handled at usage site")
}

func (v *Maybe[T]) DeserializeTL(r io.Reader) error {
	return fmt.Errorf("maybe deserialization should be handled at usage site")
}
