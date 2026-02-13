package codec

import (
	"bytes"
	"errors"
	"io"
	"sync"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/mtproto"
)

var (
	ErrUnknownConstructor = errors.New("codec: unknown constructor")
	ErrNilObject          = errors.New("codec: nil object")
)

type ConstructorFunc func() tlrpc.TLObject

type Registry struct {
	mu           sync.RWMutex
	constructors map[uint32]ConstructorFunc
	methods      map[string]ConstructorFunc
}

func NewRegistry() *Registry {
	return &Registry{
		constructors: make(map[uint32]ConstructorFunc),
		methods:      make(map[string]ConstructorFunc),
	}
}

func (r *Registry) RegisterConstructor(id uint32, fn ConstructorFunc) {
	r.mu.Lock()
	r.constructors[id] = fn
	r.mu.Unlock()
}

func (r *Registry) RegisterMethod(name string, fn ConstructorFunc) {
	r.mu.Lock()
	r.methods[name] = fn
	r.mu.Unlock()
}

func (r *Registry) LookupConstructor(id uint32) (ConstructorFunc, bool) {
	r.mu.RLock()
	fn, ok := r.constructors[id]
	r.mu.RUnlock()
	return fn, ok
}

func (r *Registry) LookupMethod(name string) (ConstructorFunc, bool) {
	r.mu.RLock()
	fn, ok := r.methods[name]
	r.mu.RUnlock()
	return fn, ok
}

type Codec struct {
	registry *Registry
}

func New(registry *Registry) *Codec {
	return &Codec{registry: registry}
}

func (c *Codec) Decode(layer int, data []byte) (tlrpc.TLObject, error) {
	if len(data) < 4 {
		return nil, io.ErrUnexpectedEOF
	}
	r := bytes.NewReader(data)
	constructorID, err := mtproto.ReadUint32(r)
	if err != nil {
		return nil, err
	}
	fn, ok := c.registry.LookupConstructor(constructorID)
	if !ok {
		return nil, ErrUnknownConstructor
	}
	obj := fn()
	if deser, ok := obj.(interface{ DeserializeTL(io.Reader) error }); ok {
		if err := deser.DeserializeTL(r); err != nil {
			return nil, err
		}
	}
	return obj, nil
}

func (c *Codec) Encode(layer int, obj tlrpc.TLObject) ([]byte, error) {
	if obj == nil {
		return nil, ErrNilObject
	}
	serializer, ok := obj.(interface{ SerializeTL(io.Writer) error })
	if !ok {
		return nil, errors.New("codec: object does not implement SerializeTL")
	}
	buf := &bytes.Buffer{}
	if err := serializer.SerializeTL(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *Codec) Registry() *Registry {
	return c.registry
}
