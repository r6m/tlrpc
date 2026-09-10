package mtproto

import (
	"errors"
	"io"
)

var (
	ErrEncodedObjectDepthLimit = errors.New("mtproto: encoded object depth limit exceeded")
	ErrEncodedObjectNodesLimit = errors.New("mtproto: encoded object node limit exceeded")
)

type encodeState struct {
	depth int
	nodes int
}

type objectWriter struct {
	io.Writer
	state *encodeState
}

func (w *objectWriter) encodeState() *encodeState { return w.state }
func (w *objectWriter) TLLayer() int              { return TLLayer(w.Writer) }

func encodeStateFromWriter(w io.Writer) *encodeState {
	if carrier, ok := w.(interface{ encodeState() *encodeState }); ok {
		return carrier.encodeState()
	}
	return nil
}

// EnterEncodeObject bounds recursive generated serialization, including direct
// codec calls without a runtime encoder. Generated methods pass the returned
// writer to their children and defer leave. Cycles fail at the depth limit.
func EnterEncodeObject(w io.Writer) (writer io.Writer, leave func(), err error) {
	state := encodeStateFromWriter(w)
	if state == nil {
		state = &encodeState{}
		w = &objectWriter{Writer: w, state: state}
	}
	if state.depth >= DefaultMaxObjectDepth {
		return nil, nil, ErrEncodedObjectDepthLimit
	}
	if state.nodes >= DefaultMaxObjectNodes {
		return nil, nil, ErrEncodedObjectNodesLimit
	}
	state.depth++
	state.nodes++
	return w, func() { state.depth-- }, nil
}
