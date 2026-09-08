package tl

import (
	"errors"
	"io"
	"math"

	"github.com/r6m/tlrpc/mtproto"
)

const (
	JSONNullID         uint32 = 0x3f6d7b68
	JSONBoolID         uint32 = 0xc7345e6a
	JSONNumberID       uint32 = 0x2be0dfa4
	JSONStringID       uint32 = 0xb71e767a
	JSONArrayID        uint32 = 0xf7444763
	JSONObjectID       uint32 = 0x99c1d49d
	jsonObjectValueID  uint32 = 0xc0de1bd9
	inputClientProxyID uint32 = 0x75588b3f
)

var ErrInitConnectionOptions = errors.New("invalid initConnection options")

type InputClientProxy struct {
	Address string
	Port    int32
}
type JSONObjectValue struct {
	Key   string
	Value JSONValue
}

// JSONValue is the MTProto JSONValue union, not a JSON-encoded string.
type JSONValue struct {
	Kind   uint32
	Bool   bool
	Number float64
	String string
	Array  []JSONValue
	Object []JSONObjectValue
}

func (m *InitConnection) readOptions(r io.Reader) error {
	m.Proxy, m.Params = nil, nil
	if m.Flags & ^uint32(3) != 0 {
		return ErrInitConnectionOptions
	}
	if m.Flags&1 != 0 {
		ctor, err := mtproto.ReadUint32(r)
		if err != nil {
			return err
		}
		if ctor != inputClientProxyID {
			return ErrInitConnectionOptions
		}
		proxy := &InputClientProxy{}
		if proxy.Address, err = mtproto.ReadString(r); err != nil {
			return err
		}
		if proxy.Port, err = mtproto.ReadInt32(r); err != nil {
			return err
		}
		if proxy.Port < 1 || proxy.Port > 65535 {
			return ErrInitConnectionOptions
		}
		m.Proxy = proxy
	}
	if m.Flags&2 != 0 {
		value := &JSONValue{}
		nodes := 0
		if err := value.read(r, 0, &nodes); err != nil {
			return err
		}
		m.Params = value
	}
	return nil
}

func (m *InitConnection) writeOptions(w io.Writer) error {
	if m.Flags & ^uint32(3) != 0 || (m.Flags&1 != 0) != (m.Proxy != nil) || (m.Flags&2 != 0) != (m.Params != nil) {
		return ErrInitConnectionOptions
	}
	if m.Proxy != nil {
		if m.Proxy.Port < 1 || m.Proxy.Port > 65535 {
			return ErrInitConnectionOptions
		}
		if err := mtproto.WriteUint32(w, inputClientProxyID); err != nil {
			return err
		}
		if err := mtproto.WriteString(w, m.Proxy.Address); err != nil {
			return err
		}
		if err := mtproto.WriteInt32(w, m.Proxy.Port); err != nil {
			return err
		}
	}
	if m.Params != nil {
		nodes := 0
		return m.Params.write(w, 0, &nodes)
	}
	return nil
}

func jsonLimit(depth int, nodes *int) error {
	*nodes++
	if depth > 16 || *nodes > 1024 {
		return ErrInitConnectionOptions
	}
	return nil
}

func (v *JSONValue) read(r io.Reader, depth int, nodes *int) error {
	if err := jsonLimit(depth, nodes); err != nil {
		return err
	}
	leave, err := mtproto.EnterObject(r)
	if err != nil {
		return err
	}
	defer leave()
	*v = JSONValue{}
	if v.Kind, err = mtproto.ReadUint32(r); err != nil {
		return err
	}
	switch v.Kind {
	case JSONNullID:
		return nil
	case JSONBoolID:
		v.Bool, err = mtproto.ReadBool(r)
	case JSONNumberID:
		v.Number, err = mtproto.ReadDouble(r)
		if math.IsNaN(v.Number) || math.IsInf(v.Number, 0) {
			return ErrInitConnectionOptions
		}
	case JSONStringID:
		v.String, err = mtproto.ReadString(r)
	case JSONArrayID:
		err = mtproto.ReadVectorBounded(r, 1024, func() error {
			var child JSONValue
			if err := child.read(r, depth+1, nodes); err != nil {
				return err
			}
			v.Array = append(v.Array, child)
			return nil
		})
	case JSONObjectID:
		err = mtproto.ReadVectorBounded(r, 1024, func() error {
			if err := jsonLimit(depth+1, nodes); err != nil {
				return err
			}
			leave, err := mtproto.EnterObject(r)
			if err != nil {
				return err
			}
			defer leave()
			ctor, err := mtproto.ReadUint32(r)
			if err != nil {
				return err
			}
			if ctor != jsonObjectValueID {
				return ErrInitConnectionOptions
			}
			var entry JSONObjectValue
			if entry.Key, err = mtproto.ReadString(r); err != nil {
				return err
			}
			if err := entry.Value.read(r, depth+1, nodes); err != nil {
				return err
			}
			v.Object = append(v.Object, entry)
			return nil
		})
	default:
		return ErrInitConnectionOptions
	}
	return err
}

func (v *JSONValue) write(w io.Writer, depth int, nodes *int) error {
	if err := jsonLimit(depth, nodes); err != nil {
		return err
	}
	if err := mtproto.WriteUint32(w, v.Kind); err != nil {
		return err
	}
	switch v.Kind {
	case JSONNullID:
		return nil
	case JSONBoolID:
		return mtproto.WriteBool(w, v.Bool)
	case JSONNumberID:
		if math.IsNaN(v.Number) || math.IsInf(v.Number, 0) {
			return ErrInitConnectionOptions
		}
		return mtproto.WriteDouble(w, v.Number)
	case JSONStringID:
		return mtproto.WriteString(w, v.String)
	case JSONArrayID:
		if len(v.Array) > 1024 {
			return ErrInitConnectionOptions
		}
		if err := mtproto.WriteVectorHeader(w, len(v.Array)); err != nil {
			return err
		}
		for i := range v.Array {
			if err := v.Array[i].write(w, depth+1, nodes); err != nil {
				return err
			}
		}
	case JSONObjectID:
		if len(v.Object) > 1024 {
			return ErrInitConnectionOptions
		}
		if err := mtproto.WriteVectorHeader(w, len(v.Object)); err != nil {
			return err
		}
		for _, entry := range v.Object {
			if err := jsonLimit(depth+1, nodes); err != nil {
				return err
			}
			if err := mtproto.WriteUint32(w, jsonObjectValueID); err != nil {
				return err
			}
			if err := mtproto.WriteString(w, entry.Key); err != nil {
				return err
			}
			if err := entry.Value.write(w, depth+1, nodes); err != nil {
				return err
			}
		}
	default:
		return ErrInitConnectionOptions
	}
	return nil
}
