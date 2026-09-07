package mtproto

import (
	"bytes"
	"io"
	"testing"
)

func TestLayerIOPreservesNestedBudgetAndLength(t *testing.T) {
	budget, err := NewDecodeBudget(DecodeLimits{})
	if err != nil {
		t.Fatal(err)
	}
	reader := WithLayerReader(NewBudgetReader(bytes.NewReader([]byte{1, 2, 3, 4}), budget), 229)
	if TLLayer(reader) != 229 || budgetFromReader(reader) != budget || reader.(interface{ Len() int }).Len() != 4 {
		t.Fatal("lost reader metadata")
	}
	var first [1]byte
	if _, err := io.ReadFull(reader, first[:]); err != nil {
		t.Fatal(err)
	}
	nested := PrependReader(first[:], reader)
	if TLLayer(nested) != 229 || budgetFromReader(nested) != budget || nested.(interface{ Len() int }).Len() != 4 {
		t.Fatal("lost nested metadata")
	}
	got, err := io.ReadAll(nested)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{1, 2, 3, 4}) {
		t.Fatal(got)
	}
	if budget.decodedBytes != 4 {
		t.Fatalf("charged prefix twice: %d", budget.decodedBytes)
	}
	wrapped := NewBudgetReader(WithLayerReader(bytes.NewReader(nil), 228), budget)
	if TLLayer(wrapped) != 228 {
		t.Fatal("budget wrapper lost layer")
	}
}

func TestLayerWriterAndPlainDefaults(t *testing.T) {
	var b bytes.Buffer
	w := WithLayerWriter(&b, 229)
	if TLLayer(w) != 229 || TLLayer(&b) != 0 || TLLayer(nil) != 0 {
		t.Fatal("incorrect writer layer")
	}
	if _, err := w.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.Bytes(), []byte{1}) {
		t.Fatal("write changed")
	}
}
