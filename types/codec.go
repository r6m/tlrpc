package types

// TLObject is the minimal object contract needed for constructor registration.
type TLObject interface {
	ConstructorID() uint32
}

// Base type constructors for MTProto core types
var baseConstructors = map[uint32]func() TLObject{
	0xbc799737: func() TLObject { return &BoolFalse{} },        // boolFalse
	0x997275b5: func() TLObject { return &BoolTrue{} },         // boolTrue
	0x3fedd339: func() TLObject { return &True{} },             // true
	0xc4b9f9bb: func() TLObject { return &Error{} },            // error
	0x56730bcc: func() TLObject { return &Null{} },             // null
	0xb5286e24: func() TLObject { s := String(""); return &s }, // string
	0x0a1cdbd1: func() TLObject { return &Bytes{} },            // bytes
	0x84c1e679: func() TLObject { return &Int128{} },           // int128
	0x7bed4774: func() TLObject { return &Int256{} },           // int256
	0x2210c154: func() TLObject { d := Double(0); return &d },  // double
}

// GetBaseConstructors returns the static constructor map for base MTProto types
func GetBaseConstructors() map[uint32]func() TLObject {
	return baseConstructors
}
