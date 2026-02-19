package tlrpc

import (
	"reflect"
)

func normalizeResponse(resp interface{}) (TLObject, error) {
	if resp == nil {
		return nil, nil
	}
	resp = derefInterfacePointer(resp)
	if resp == nil {
		return nil, nil
	}
	obj, ok := resp.(TLObject)
	if !ok {
		return nil, NewInternalError("response does not implement TLObject")
	}
	return obj, nil
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
