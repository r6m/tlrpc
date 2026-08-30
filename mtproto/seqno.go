package mtproto

import "sync"

// SeqNoGenerator provides MTProto seq_no behavior for a single session.
type SeqNoGenerator struct {
	mu  sync.Mutex
	seq int32
}

// NewSeqNoGenerator creates a new seq_no generator.
func NewSeqNoGenerator(initial int32) *SeqNoGenerator {
	return &SeqNoGenerator{seq: initial}
}

// Next returns the next seq_no based on MTProto rules.
// Content-related messages increment the sequence and return an odd seq_no.
// Non-content messages return an even seq_no without incrementing.
func (g *SeqNoGenerator) Next(contentRelated bool) int32 {
	g.mu.Lock()
	defer g.mu.Unlock()

	if contentRelated {
		value := g.seq*2 + 1
		g.seq++
		return value
	}
	return g.seq * 2
}

// Value returns the current sequence value.
func (g *SeqNoGenerator) Value() int32 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.seq
}
