// Package layer provides layer negotiation.
package layer

import (
	"errors"
)

// NegotiationResult represents the result of layer negotiation.
type NegotiationResult struct {
	Layer   Layer
	Version int
}

// Negotiate determines which layer to use for a client.
type Negotiator struct {
	registry *Registry
}

// NewNegotiator creates a new negotiator.
func NewNegotiator(registry *Registry) *Negotiator {
	return &Negotiator{
		registry: registry,
	}
}

// Negotiate determines the layer to use.
func (n *Negotiator) Negotiate(clientVersion int) (*NegotiationResult, error) {
	if clientVersion <= 0 {
		// Use latest if no version specified
		layer := n.registry.GetLatest()
		if layer == nil {
			return nil, ErrNoLayersRegistered
		}
		return &NegotiationResult{
			Layer:   layer,
			Version: layer.Version(),
		}, nil
	}

	layer, exists := n.registry.Get(clientVersion)
	if !exists {
		return nil, ErrUnsupportedLayer
	}

	return &NegotiationResult{
		Layer:   layer,
		Version: clientVersion,
	}, nil
}

// Errors
var (
	ErrNoLayersRegistered = errors.New("layer: no layers registered")
	ErrUnsupportedLayer   = errors.New("layer: unsupported layer version")
)