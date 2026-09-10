package mtproto

import "io"

// TLLayer returns the layer attached to an encoder or decoder. Zero denotes
// the generated package's default representation, preserving unlayered use.
func TLLayer(value any) int {
	if layered, ok := value.(interface{ TLLayer() int }); ok {
		return layered.TLLayer()
	}
	return 0
}

type layerReader struct {
	io.Reader
	layer int
}

func (r *layerReader) TLLayer() int                { return r.layer }
func (r *layerReader) DecodeBudget() *DecodeBudget { return budgetFromReader(r.Reader) }

type sizedLayerReader struct {
	*layerReader
	sized interface{ Len() int }
}

func (r *sizedLayerReader) Len() int { return r.sized.Len() }

// WithLayerReader propagates layer selection through nested generated decoders
// without replacing the aggregate decode budget or remaining-length checks.
func WithLayerReader(r io.Reader, layer int) io.Reader {
	wrapped := &layerReader{Reader: r, layer: layer}
	if sized, ok := r.(interface{ Len() int }); ok {
		return &sizedLayerReader{layerReader: wrapped, sized: sized}
	}
	return wrapped
}

type layerWriter struct {
	io.Writer
	layer int
}

func (w *layerWriter) TLLayer() int              { return w.layer }
func (w *layerWriter) encodeState() *encodeState { return encodeStateFromWriter(w.Writer) }

// WithLayerWriter carries the output layer through nested generated encoders.
func WithLayerWriter(w io.Writer, layer int) io.Writer { return &layerWriter{Writer: w, layer: layer} }

func (r *BudgetReader) TLLayer() int {
	if r == nil {
		return 0
	}
	return TLLayer(r.reader)
}
func (r *prependedBudgetReader) TLLayer() int {
	if r == nil {
		return 0
	}
	return TLLayer(r.reader)
}
