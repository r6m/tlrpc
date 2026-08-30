package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLayerDifference(t *testing.T) {
	input := `// an ordinary schema comment
  // @tlrpc remove constructor legacy.item
// @tlrpc remove function legacy.get
---types---
item#00000001 value:string = Item; // declaration comment
---functions---
items.get#00000002 id:long = Item;`

	difference, err := ParseLayerDifference(input, 101, "layer-101.tl")
	require.NoError(t, err)

	assert.Equal(t, 101, difference.Layer)
	assert.Equal(t, "layer-101.tl", difference.Source)
	assert.Equal(t, []string{"legacy.item"}, difference.RemoveConstructors)
	assert.Equal(t, []string{"legacy.get"}, difference.RemoveFunctions)
	require.NotNil(t, difference.Schema)
	assert.Equal(t, 101, difference.Schema.Layer)
	require.Len(t, difference.Schema.Constructors, 1)
	assert.Equal(t, "item", difference.Schema.Constructors[0].Name)
	require.Len(t, difference.Schema.Functions, 1)
	assert.Equal(t, "items.get", difference.Schema.Functions[0].Name)
}

func TestParseLayerDifferenceRejectsMalformedDirective(t *testing.T) {
	_, err := ParseLayerDifference("// @tlrpc remove constructor", 101, "bad.tl")
	require.ErrorContains(t, err, "invalid directive")

	_, err = ParseLayerDifference("", 0, "bad.tl")
	require.ErrorContains(t, err, "layer must be positive")
}

func TestResolveLayerReplacesAppendsRemovesAndRebuildsTypes(t *testing.T) {
	base := mustParseLayerSchema(t, 100, `---types---
item#00000001 flags:# values:flags.0?vector<int> = Item;
itemFull#00000002 = Item;
legacy#00000003 = Legacy;
---functions---
items.get#00000010 id:long = Item;
legacy.get#00000011 = Legacy;`)
	difference101 := mustParseDifference(t, 101, `
// @tlrpc remove constructor legacy
// @tlrpc remove function legacy.get
---types---
itemFull#00000004 title:string = Renamed;
itemNew#00000005 = Item;
---functions---
items.get#00000012 id:long = Renamed;
items.list#00000013 = Item;`)
	difference102 := mustParseDifference(t, 102, `
// @tlrpc remove constructor item
---types---
last#00000006 = Last;`)

	resolved, err := ResolveLayer(base, 100, 101, []LayerDifference{difference101, difference102})
	require.NoError(t, err)

	assert.Equal(t, 101, resolved.Layer)
	assert.Equal(t, []string{"item", "itemFull", "itemNew"}, constructorNames(resolved.Constructors))
	assert.Equal(t, []string{"items.get", "items.list"}, functionNames(resolved.Functions))
	assert.Equal(t, uint32(0x00000004), resolved.Constructors[1].ID)
	assert.Equal(t, "Renamed", resolved.Constructors[1].ResultType.FullName())
	assert.Equal(t, uint32(0x00000012), resolved.Functions[0].ID)

	require.Len(t, resolved.Types, 2)
	assert.Equal(t, "Item", resolved.Types[0].Name)
	assert.Equal(t, []string{"item", "itemNew"}, constructorNames(resolved.Types[0].Constructors))
	assert.True(t, resolved.Types[0].IsUnion)
	assert.Equal(t, "Renamed", resolved.Types[1].Name)
	assert.False(t, resolved.Types[1].IsUnion)
	assert.Equal(t, map[string]bool{"Item": true}, resolved.UnionTypes)

	// The later delta is supplied and validated structurally, but is not applied
	// when resolving an earlier target.
	assert.NotContains(t, constructorNames(resolved.Constructors), "last")
}

func TestResolveLayerReturnsDetachedSchemas(t *testing.T) {
	base := mustParseLayerSchema(t, 100, `---types---
item#00000001 flags:# values:flags.0?vector<int> = Item;`)
	difference := mustParseDifference(t, 101, `---types---
added#00000002 value:vector<string> = Added;`)

	baseResult, err := ResolveLayer(base, 100, 100, []LayerDifference{difference})
	require.NoError(t, err)
	layerResult, err := ResolveLayer(base, 100, 101, []LayerDifference{difference})
	require.NoError(t, err)

	*baseResult.Constructors[0].Params[1].Type.FlagBit = 9
	baseResult.Constructors[0].Params[1].Type.Generic.Name = "long"
	layerResult.Constructors[1].Params[0].Type.Generic.Name = "bytes"
	layerResult.Types[0].Constructors[0].Name = "mutated"

	assert.Equal(t, 0, *base.Constructors[0].Params[1].Type.FlagBit)
	assert.Equal(t, "int", base.Constructors[0].Params[1].Type.Generic.Name)
	assert.Equal(t, "string", difference.Schema.Constructors[0].Params[0].Type.Generic.Name)
	assert.Equal(t, "item", layerResult.Constructors[0].Name)
	assert.Equal(t, "item", base.Types[0].Constructors[0].Name)
}

func TestResolveLayerValidatesLayerSequenceAndTarget(t *testing.T) {
	base := mustParseLayerSchema(t, 100, `base#00000001 = Base;`)
	empty101 := LayerDifference{Layer: 101, Schema: NewSchema(101)}
	empty102 := LayerDifference{Layer: 102, Schema: NewSchema(102)}

	tests := []struct {
		name        string
		target      int
		differences []LayerDifference
		want        string
	}{
		{name: "layer not above base", target: 100, differences: []LayerDifference{{Layer: 100, Schema: NewSchema(100)}}, want: "must be greater than base"},
		{name: "decreasing", target: 101, differences: []LayerDifference{empty102, empty101}, want: "strictly increasing and unique"},
		{name: "duplicate", target: 101, differences: []LayerDifference{empty101, empty101}, want: "strictly increasing and unique"},
		{name: "unknown target", target: 103, differences: []LayerDifference{empty101, empty102}, want: "neither base layer"},
		{name: "below base", target: 99, want: "below base layer"},
		{name: "nil delta schema", target: 101, differences: []LayerDifference{{Layer: 101}}, want: "schema is nil"},
		{name: "non-positive base", target: 100, differences: nil, want: "base layer must be positive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseLayer := 100
			if tt.name == "non-positive base" {
				baseLayer = 0
			}
			_, err := ResolveLayer(base, baseLayer, tt.target, tt.differences)
			require.ErrorContains(t, err, tt.want)
		})
	}

	_, err := ResolveLayer(nil, 100, 100, nil)
	require.ErrorContains(t, err, "base schema is nil")

	_, err = ResolveLayer(base, 101, 101, nil)
	require.ErrorContains(t, err, "does not match declared base layer")

	mismatched := LayerDifference{Layer: 101, Schema: NewSchema(102)}
	_, err = ResolveLayer(base, 100, 101, []LayerDifference{mismatched})
	require.ErrorContains(t, err, "does not match difference layer")
}

func TestResolveLayerRejectsMissingRemoveTargets(t *testing.T) {
	base := mustParseLayerSchema(t, 100, `---types---
base#00000001 = Base;
---functions---
base.get#00000002 = Base;`)

	constructorRemoval := mustParseDifference(t, 101, `// @tlrpc remove constructor missing`)
	_, err := ResolveLayer(base, 100, 101, []LayerDifference{constructorRemoval})
	require.ErrorContains(t, err, `cannot remove missing constructor "missing"`)

	functionRemoval := mustParseDifference(t, 101, `// @tlrpc remove function missing.get`)
	_, err = ResolveLayer(base, 100, 101, []LayerDifference{functionRemoval})
	require.ErrorContains(t, err, `cannot remove missing function "missing.get"`)

	// Missing targets in layers after the requested target are not applied.
	lateRemoval := mustParseDifference(t, 102, `// @tlrpc remove constructor missing`)
	_, err = ResolveLayer(base, 100, 101, []LayerDifference{
		{Layer: 101, Schema: NewSchema(101)},
		lateRemoval,
	})
	require.NoError(t, err)
}

func TestParseLayerDifferenceRejectsDuplicateNames(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "constructors",
			input: `same#00000001 = One;
same#00000002 = Two;`,
			want: `duplicate constructor name "same"`,
		},
		{
			name: "functions",
			input: `---functions---
same.get#00000001 = One;
same.get#00000002 = Two;`,
			want: `duplicate function name "same.get"`,
		},
		{
			name: "remove directives",
			input: `// @tlrpc remove constructor same
// @tlrpc remove constructor same`,
			want: `duplicate constructor name "same" inside delta`,
		},
		{
			name: "declaration and removal",
			input: `// @tlrpc remove function same.get
---functions---
same.get#00000001 = One;`,
			want: `duplicate function name "same.get" inside delta`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseLayerDifference(tt.input, 101, "duplicate.tl")
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestResolveLayerRejectsIDCollisionsAcrossNames(t *testing.T) {
	base := mustParseLayerSchema(t, 100, `---types---
base#00000001 = Base;
---functions---
base.get#00000002 = Base;`)

	constructorCollision := mustParseDifference(t, 101, `other#00000001 = Other;`)
	_, err := ResolveLayer(base, 100, 101, []LayerDifference{constructorCollision})
	require.ErrorContains(t, err, "constructor ID 0x00000001 collides")

	functionCollision := mustParseDifference(t, 101, `---functions---
other.get#00000002 = Base;`)
	_, err = ResolveLayer(base, 100, 101, []LayerDifference{functionCollision})
	require.ErrorContains(t, err, "function ID 0x00000002 collides")

	_, err = ParseLayerDifference(`first#00000003 = First;
second#00000003 = Second;`, 101, "collision.tl")
	require.ErrorContains(t, err, "constructor ID 0x00000003 collides")
}

func mustParseLayerSchema(t *testing.T, layer int, input string) *Schema {
	t.Helper()
	schema, err := NewParser(input).ParseWithLayer(layer)
	require.NoError(t, err)
	return schema
}

func mustParseDifference(t *testing.T, layer int, input string) LayerDifference {
	t.Helper()
	difference, err := ParseLayerDifference(input, layer, "test.tl")
	require.NoError(t, err)
	return difference
}

func constructorNames(constructors []Constructor) []string {
	names := make([]string, len(constructors))
	for i, constructor := range constructors {
		names[i] = constructor.Name
	}
	return names
}

func functionNames(functions []FuncDecl) []string {
	names := make([]string, len(functions))
	for i, function := range functions {
		names[i] = function.Name
	}
	return names
}
