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
auth.signUp#80eee427 phone:string = Result; // historical form
auth.signUp#aac7b717 flags:# phone:string = Result;
// Provenance: official Telegram Android tlscheme/158.json.
// @tlrpc variant-layer 158
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
	assert.Equal(t, 228, variants[0].MinLayer)
	assert.Equal(t, 0, variants[0].MaxLayer)
	assert.Equal(t, 172, variants[0].VariantLayer)
	assert.Equal(t, 0, variants[1].VariantLayer)
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
			name: "same ID",
			input: `---functions---
// @tlrpc variant-layer 172
auth.signUp#80eee427 = X;
auth.signUp#80eee427 = X;`,
			want: "duplicate function",
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
			want: "only to functions",
		},
		{
			name: "future variant",
			input: `---functions---
// @tlrpc variant-layer 229
auth.signUp#80eee427 = X;
auth.signUp#aac7b717 = X;`,
			want: "exceeds baseline layer",
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
	require.ErrorContains(t, err, "requires a positive baseline layer")
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
