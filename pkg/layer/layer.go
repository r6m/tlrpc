// Package layer provides layer support for MTProto.
package layer

import (
	"github.com/r6m/tlrpc/pkg/mtproto"
)

// Layer represents a Telegram API layer.
type Layer interface {
	// Version returns the layer version.
	Version() int

	// Deserialize deserializes data into a TLObject.
	Deserialize(constructorID uint32, data []byte) (mtproto.TLObject, error)

	// Serialize serializes a TLObject into bytes.
	Serialize(obj mtproto.TLObject) ([]byte, error)

	// GetConstructorID returns the constructor ID for an object.
	GetConstructorID(obj mtproto.TLObject) uint32
}

// BaseLayer provides a base implementation of Layer.
type BaseLayer struct {
	version      int
	constructors mtproto.ConstructorMap
}

// NewBaseLayer creates a new base layer.
func NewBaseLayer(version int) *BaseLayer {
	return &BaseLayer{
		version:      version,
		constructors: mtproto.NewConstructorMap(),
	}
}

// Version returns the layer version.
func (l *BaseLayer) Version() int {
	return l.version
}

// Deserialize deserializes data into a TLObject.
func (l *BaseLayer) Deserialize(constructorID uint32, data []byte) (mtproto.TLObject, error) {
	obj, exists := l.constructors.Create(constructorID)
	if !exists {
		return nil, ErrUnknownConstructor
	}

	// TODO: Implement proper deserialization
	return obj, nil
}

// Serialize serializes a TLObject into bytes.
func (l *BaseLayer) Serialize(obj mtproto.TLObject) ([]byte, error) {
	// TODO: Implement proper serialization
	return []byte{}, nil
}

// GetConstructorID returns the constructor ID for an object.
func (l *BaseLayer) GetConstructorID(obj mtproto.TLObject) uint32 {
	return obj.ConstructorID()
}

// RegisterConstructor registers a constructor.
func (l *BaseLayer) RegisterConstructor(constructorID uint32, constructor func() mtproto.TLObject) {
	l.constructors.Register(constructorID, constructor)
}

// Errors
var (
	ErrUnknownConstructor = mtproto.ErrUnknownConstructor
)
