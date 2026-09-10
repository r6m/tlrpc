package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBaselineSchemaAssignsHistoricalFunctionVariants(t *testing.T) {
	schema, err := ParseBaselineSchema(`---types---
result#00000001 = Result;
---functions---
// Provenance: official Telegram Android tlscheme/172.json.
// @tlrpc variant-layer 172
// @tlrpc accept-layers 228-228
auth.signUp#80eee427 phone:string = Result; // historical form
auth.signUp#aac7b717 flags:# phone:string = Result;
// Provenance: official Telegram Android tlscheme/158.json.
// @tlrpc variant-layer 158
// @tlrpc accept-layers 228-228
updates.getDifference#25939651 pts:int = Result;
updates.getDifference#19c2f763 flags:# pts:int = Result;`, 228, "baseline.tl")
	require.NoError(t, err)
	require.Len(t, schema.Functions, 4)
	assert.Equal(t, 172, schema.Functions[0].VariantLayer)
	assert.Zero(t, schema.Functions[1].VariantLayer)
	assert.Equal(t, 158, schema.Functions[2].VariantLayer)
	assert.Zero(t, schema.Functions[3].VariantLayer)

	resolved, err := ResolveLayers(schema, 228, nil)
	require.NoError(t, err)
	variants := functionsNamedForBaselineTest(resolved.Schema.Functions, "auth.signUp")
	require.Len(t, variants, 2)
	assert.Equal(t, 0, variants[0].VariantLayer)
	assert.Equal(t, []LayerInterval{{MinLayer: 228}}, variants[0].Intervals)
	assert.Equal(t, 172, variants[1].VariantLayer)
	assert.Equal(t, []LayerInterval{{MinLayer: 228, MaxLayer: 228}}, variants[1].Intervals)
}

func TestParseBaselineSchemaRejectsAmbiguousVariants(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "missing annotation",
			input: `---functions---
auth.signUp#80eee427 = X;
auth.signUp#aac7b717 = X;`,
			want: "exactly one unannotated canonical",
		},
		{
			name: "annotation without canonical",
			input: `---functions---
// @tlrpc variant-layer 172
auth.signUp#80eee427 = X;`,
			want: "no canonical declaration",
		},
		{
			name: "historical acceptance required",
			input: `---functions---
// @tlrpc variant-layer 172
auth.signUp#80eee427 = X;
auth.signUp#80eee427 = X;`,
			want: "requires accept-layers",
		},
		{
			name: "not immediate",
			input: `---functions---
// @tlrpc variant-layer 172

auth.signUp#80eee427 = X;`,
			want: "immediately precede",
		},
		{
			name: "constructor annotation",
			input: `---types---
// @tlrpc variant-layer 172
item#00000001 = Item;`,
			want: "function declaration",
		},
		{
			name: "future variant",
			input: `---functions---
// @tlrpc variant-layer 229
auth.signUp#80eee427 = X;
auth.signUp#aac7b717 = X;`,
			want: "invalid variant-layer",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseBaselineSchema(test.input, 228, "baseline.tl")
			require.ErrorContains(t, err, test.want)
		})
	}
	_, err := ParseBaselineSchema(`---functions---
// @tlrpc variant-layer 172
auth.signUp#80eee427 = X;
auth.signUp#aac7b717 = X;`, 0, "baseline.tl")
	require.ErrorContains(t, err, "invalid variant-layer")
}

func TestParseBaselineSchemaValidatesAcceptIntervals(t *testing.T) {
	for _, value := range []string{"228", "229-228", "228-229,229-230", "0-1"} {
		_, err := ParseBaselineSchema("---functions---\n// @tlrpc variant-layer 172\n// @tlrpc accept-layers "+value+"\na#1 = X;\na#2 = X;", 228, "baseline.tl")
		require.Error(t, err, value)
	}
}

func TestSelectBaselineLayerFiltersHistoricalDeclarations(t *testing.T) {
	schema, err := ParseBaselineSchema(`---functions---
// @tlrpc variant-layer 172
// @tlrpc accept-layers 228-229
a#1 old:int = X;
a#2 current:int = X;`, 228, "baseline.tl")
	require.NoError(t, err)
	selected, err := SelectBaselineLayer(schema, 230)
	require.NoError(t, err)
	require.Len(t, selected.Functions, 1)
	assert.Zero(t, selected.Functions[0].VariantLayer)
	selected, err = SelectBaselineLayer(schema, 229)
	require.NoError(t, err)
	require.Len(t, selected.Functions, 2)
}

func functionsNamedForBaselineTest(functions []FuncDecl, name string) []FuncDecl {
	var matches []FuncDecl
	for _, function := range functions {
		if function.Name == name {
			matches = append(matches, function)
		}
	}
	return matches
}
