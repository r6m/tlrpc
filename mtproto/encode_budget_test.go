package mtproto

import (
	"errors"
	"io"
	"testing"
)

func TestEncodeBudgetSurvivesLayerWrappers(t *testing.T) {
	w, leave, err := EnterEncodeObject(WithLayerWriter(io.Discard, 229))
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	if TLLayer(w) != 229 {
		t.Fatal("lost effective layer")
	}
	leaves := []func(){}
	for i := 1; i < DefaultMaxObjectDepth; i++ {
		var done func()
		w, done, err = EnterEncodeObject(WithLayerWriter(w, 229))
		if err != nil {
			t.Fatal(err)
		}
		leaves = append(leaves, done)
	}
	if _, _, err := EnterEncodeObject(w); !errors.Is(err, ErrEncodedObjectDepthLimit) {
		t.Fatalf("depth error: %v", err)
	}
	for i := len(leaves) - 1; i >= 0; i-- {
		leaves[i]()
	}
	_, done, err := EnterEncodeObject(w)
	if err != nil {
		t.Fatalf("balanced siblings rejected: %v", err)
	}
	done()
}

func TestEncodeBudgetCountsSiblingNodes(t *testing.T) {
	w, leave, err := EnterEncodeObject(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	for i := 1; i < DefaultMaxObjectNodes; i++ {
		_, done, err := EnterEncodeObject(w)
		if err != nil {
			t.Fatal(err)
		}
		done()
	}
	if _, _, err := EnterEncodeObject(w); !errors.Is(err, ErrEncodedObjectNodesLimit) {
		t.Fatalf("node error: %v", err)
	}
}
