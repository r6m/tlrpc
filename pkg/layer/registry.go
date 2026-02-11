// Package layer provides layer registry.
package layer

import (
	"sync"
)

// Registry manages layers.
type Registry struct {
	mu     sync.RWMutex
	layers map[int]Layer
}

// NewRegistry creates a new layer registry.
func NewRegistry() *Registry {
	return &Registry{
		layers: make(map[int]Layer),
	}
}

// Register registers a layer.
func (r *Registry) Register(layer Layer) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.layers[layer.Version()] = layer
}

// Get returns a layer by version.
func (r *Registry) Get(version int) (Layer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	layer, exists := r.layers[version]
	return layer, exists
}

// GetAll returns all registered layers.
func (r *Registry) GetAll() map[int]Layer {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[int]Layer, len(r.layers))
	for k, v := range r.layers {
		result[k] = v
	}
	return result
}

// GetLatest returns the highest version layer.
func (r *Registry) GetLatest() Layer {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var latest Layer
	maxVersion := 0

	for _, layer := range r.layers {
		if layer.Version() > maxVersion {
			maxVersion = layer.Version()
			latest = layer
		}
	}

	return latest
}

// IsSupported checks if a layer version is supported.
func (r *Registry) IsSupported(version int) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.layers[version]
	return exists
}