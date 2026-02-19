package tlrpc

import (
	"fmt"
	"io"
	"reflect"

	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/types"
)

func normalizeResponse(resp interface{}) (TLObject, error) {
	if resp == nil {
		return nil, nil
	}
	resp = derefInterfacePointer(resp)
	if resp == nil {
		return nil, nil
	}
	switch v := resp.(type) {
	case TLObject:
		return v, nil
	case []byte:
		bytes := types.Bytes(v)
		return &bytes, nil
	case string:
		str := types.String(v)
		return &str, nil
	}
	rv := reflect.ValueOf(resp)
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		return newTLVector(resp)
	}
	return nil, NewInternalError("response does not implement TLObject")
}

func derefInterfacePointer(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	v := reflect.ValueOf(value)
	if v.Kind() != reflect.Ptr {
		return value
	}
	if v.IsNil() {
		return nil
	}
	if v.Elem().Kind() != reflect.Interface {
		return value
	}
	inner := v.Elem()
	if inner.IsNil() {
		return nil
	}
	return inner.Interface()
}

type tlVector struct {
	items []interface{}
}

func newTLVector(value interface{}) (TLObject, error) {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, NewInternalError("response is not a slice")
	}
	items := make([]interface{}, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		items[i] = rv.Index(i).Interface()
	}
	return &tlVector{items: items}, nil
}

func (v *tlVector) ConstructorID() uint32 { return mtproto.VectorConstructorID }
func (v *tlVector) Method() string        { return "" }
func (v *tlVector) TLName() string        { return "vector" }

func (v *tlVector) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteVectorHeader(w, len(v.items)); err != nil {
		return err
	}
	for _, item := range v.items {
		if err := writeVectorItem(w, item); err != nil {
			return err
		}
	}
	return nil
}

func (v *tlVector) DeserializeTL(io.Reader) error {
	return fmt.Errorf("tlVector DeserializeTL not implemented")
}

func writeVectorItem(w io.Writer, item interface{}) error {
	item = derefInterfacePointer(item)
	if item == nil {
		return NewInternalError("vector item is nil")
	}
	switch v := item.(type) {
	case TLObject:
		return v.(interface{ SerializeTL(io.Writer) error }).SerializeTL(w)
	case string:
		return mtproto.WriteString(w, v)
	case []byte:
		return mtproto.WriteBytes(w, v)
	case int32:
		return mtproto.WriteInt32(w, v)
	case int64:
		return mtproto.WriteInt64(w, v)
	case uint32:
		return mtproto.WriteUint32(w, v)
	case uint64:
		return mtproto.WriteUint64(w, v)
	case bool:
		return mtproto.WriteBool(w, v)
	case int:
		return mtproto.WriteInt32(w, int32(v))
	default:
		return NewInternalError("unsupported vector item type")
	}
}
