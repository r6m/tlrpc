// Package mtproto provides TLObject interface.
package mtproto

import (
	"errors"
)

// Errors
var (
	ErrUnknownConstructor = errors.New("mtproto: unknown constructor")
)

// TLObject represents a Telegram TL object.
type TLObject interface {
	// ConstructorID returns the constructor ID of the object.
	ConstructorID() uint32

	// Method returns the method name if this is an RPC call.
	Method() string
}

// ConstructorMap maps constructor IDs to object types.
type ConstructorMap map[uint32]func() TLObject

// NewConstructorMap creates a new constructor map.
func NewConstructorMap() ConstructorMap {
	return make(ConstructorMap)
}

// Register registers a constructor.
func (m ConstructorMap) Register(constructorID uint32, constructor func() TLObject) {
	m[constructorID] = constructor
}

// Create creates an object from a constructor ID.
func (m ConstructorMap) Create(constructorID uint32) (TLObject, bool) {
	constructor, exists := m[constructorID]
	if !exists {
		return nil, false
	}
	return constructor(), true
}

// BaseObject provides a base implementation of TLObject.
type BaseObject struct {
	constructorID uint32
	method        string
}

// NewBaseObject creates a new base object.
func NewBaseObject(constructorID uint32, method string) BaseObject {
	return BaseObject{
		constructorID: constructorID,
		method:        method,
	}
}

// ConstructorID returns the constructor ID.
func (o BaseObject) ConstructorID() uint32 {
	return o.constructorID
}

// Method returns the method name.
func (o BaseObject) Method() string {
	return o.method
}