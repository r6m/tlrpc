// Package tlrpc provides metadata support similar to gRPC metadata.
package tlrpc

import (
	"context"
	"strings"
)

type metadataKey struct{}

// MD is a mapping from metadata keys to values. Keys are case-insensitive.
type MD map[string][]string

// NewMD creates an MD from a given key-value map.
func NewMD(md map[string]string) MD {
	out := make(MD, len(md))
	for k, v := range md {
		out[strings.ToLower(k)] = []string{v}
	}
	return out
}

// Get obtains the values for a given key.
func (md MD) Get(key string) []string {
	if md == nil {
		return nil
	}
	return md[strings.ToLower(key)]
}

// Set sets the value of a given key with a slice of values.
func (md MD) Set(key string, values ...string) {
	if md == nil {
		return
	}
	md[strings.ToLower(key)] = values
}

// Append adds the values to key, not overwriting what was already there.
func (md MD) Append(key string, values ...string) {
	if md == nil {
		return
	}
	key = strings.ToLower(key)
	md[key] = append(md[key], values...)
}

// Delete removes the values for a given key.
func (md MD) Delete(key string) {
	if md == nil {
		return
	}
	delete(md, strings.ToLower(key))
}

// Len returns the number of items in md.
func (md MD) Len() int {
	return len(md)
}

// Copy returns a copy of md.
func (md MD) Copy() MD {
	out := make(MD, len(md))
	for k, v := range md {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// NewIncomingContext creates a new context with incoming metadata attached.
func NewIncomingContext(ctx context.Context, md MD) context.Context {
	return context.WithValue(ctx, metadataKey{}, md)
}

// NewOutgoingContext creates a new context with outgoing metadata attached.
func NewOutgoingContext(ctx context.Context, md MD) context.Context {
	return context.WithValue(ctx, metadataKey{}, md)
}

// FromIncomingContext returns the incoming metadata in ctx if it exists.
func FromIncomingContext(ctx context.Context) (MD, bool) {
	md, ok := ctx.Value(metadataKey{}).(MD)
	return md, ok
}

// FromOutgoingContext returns the outgoing metadata in ctx if it exists.
func FromOutgoingContext(ctx context.Context) (MD, bool) {
	md, ok := ctx.Value(metadataKey{}).(MD)
	return md, ok
}

// AppendToOutgoingContext returns a new context with the provided kv merged
// with any existing metadata in the context.
func AppendToOutgoingContext(ctx context.Context, kv ...string) context.Context {
	if len(kv)%2 == 1 {
		panic("metadata: AppendToOutgoingContext got an odd number of key-value pairs")
	}
	md, _ := FromOutgoingContext(ctx)
	if md == nil {
		md = make(MD)
	} else {
		md = md.Copy()
	}
	for i := 0; i < len(kv); i += 2 {
		key, val := kv[i], kv[i+1]
		md[key] = append(md[key], val)
	}
	return NewOutgoingContext(ctx, md)
}
