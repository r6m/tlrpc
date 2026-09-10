package parser

import (
	"fmt"
	"os"
	"sort"
	"strings"
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

func TestResolveLayerAllowsOnlyMarkedSerializerPrefixIDPair(t *testing.T) {
	base := mustParseLayerSchema(t, 228, `---types---
error#c4b9f9bb code:int text:string = Error;
---functions---
invokeWithBusinessConnectionPrefix#dd289f8e connection_id:string = Error;
invokeWithBusinessConnection#dd289f8e {X:Type} connection_id:string query:!X = X;`)

	resolved, err := ResolveLayer(base, 228, 228, nil)
	require.NoError(t, err)
	require.Len(t, resolved.Functions, 2)
	assert.True(t, resolved.Functions[0].IsHelper)

	collision := mustParseLayerSchema(t, 228, `---types---
result#00000001 = Result;
---functions---
first#00000002 = Result;
second#00000002 = Result;`)
	_, err = ResolveLayer(collision, 228, 228, nil)
	require.ErrorContains(t, err, "function ID 0x00000002 collides")
}

func TestResolveLayersPreservesVariantsRangesAndNestedTypeVersions(t *testing.T) {
	base := mustParseLayerSchema(t, 228, `---types---
child#00000001 value:int = Child;
childRemoved#00000002 = Child;
container#00000003 child:Child = Container;
result#00000004 = Result;
---functions---
auth.signUp#80eee427 name:string = Result;
messages.forward#00000020 flags:# silent:flags.0?true = Container;`)
	difference := mustParseDifference(t, 229, `
// @tlrpc remove constructor childRemoved
---types---
child#00000001 flags:# value:int label:flags.0?string = Child;
currentNested#00000005 value:string = Nested;
---functions---
auth.signUp#aac7b717 name:string = Result;
messages.forward#00000020 flags:# silent:flags.0?true from_ephemeral:flags.1?true = Container;
messages.newMethod#00000021 = Result;`)

	layered, err := ResolveLayers(base, 228, []LayerDifference{difference})
	require.NoError(t, err)
	assert.Equal(t, 229, layered.MaxLayer)
	assert.Equal(t, []int{228, 229}, layered.Layers)

	childVariants := constructorsNamed(layered.Schema.Constructors, "child")
	require.Len(t, childVariants, 2, "optional additions are distinct fixed contracts")
	assert.Equal(t, 0, childVariants[0].VariantLayer)
	require.Len(t, childVariants[0].Params, 1)
	assert.Equal(t, 229, childVariants[1].VariantLayer)
	require.Len(t, childVariants[1].Params, 3)

	removed := constructorsNamed(layered.Schema.Constructors, "childRemoved")
	require.Len(t, removed, 1)
	assert.Equal(t, 228, removed[0].Intervals[0].MinLayer)
	assert.Equal(t, 228, removed[0].Intervals[0].MaxLayer)
	introduced := constructorsNamed(layered.Schema.Constructors, "currentNested")
	require.Len(t, introduced, 1)
	assert.Equal(t, 229, introduced[0].Intervals[0].MinLayer, "a unique constructor introduced after the base must reject older sessions")
	assert.Equal(t, 0, introduced[0].Intervals[0].MaxLayer)

	containerVariants := constructorsNamed(layered.Schema.Constructors, "container")
	require.Len(t, containerVariants, 1, "compatible nested supersets must not force parent variants")

	forwardVariants := functionsNamed(layered.Schema.Functions, "messages.forward")
	require.Len(t, forwardVariants, 2)
	assert.Equal(t, 228, forwardVariants[0].Intervals[0].MaxLayer)
	assert.Equal(t, 229, forwardVariants[1].Intervals[0].MinLayer)
	assert.Equal(t, 229, forwardVariants[1].VariantLayer)

	signupVariants := functionsNamed(layered.Schema.Functions, "auth.signUp")
	require.Len(t, signupVariants, 2)
	assert.Equal(t, 228, signupVariants[0].Intervals[0].MinLayer)
	assert.Equal(t, 228, signupVariants[0].Intervals[0].MaxLayer)
	assert.Equal(t, 229, signupVariants[1].Intervals[0].MinLayer, "a changed method's new unique ID must reject the base layer")
	assert.Equal(t, 0, signupVariants[1].Intervals[0].MaxLayer)
	newMethods := functionsNamed(layered.Schema.Functions, "messages.newMethod")
	require.Len(t, newMethods, 1)
	assert.Equal(t, 229, newMethods[0].Intervals[0].MinLayer, "a newly introduced unique method must reject the base layer")
	assert.Equal(t, 0, newMethods[0].Intervals[0].MaxLayer)
}

func TestResolveLayersClosesTransitiveConcreteOutputRanges(t *testing.T) {
	base := mustParseLayerSchema(t, 228, `---types---
child#00000001 value:int = Child;
parent#00000002 child:Child = Parent;`)
	difference := mustParseDifference(t, 229, `---types---
child#00000003 value:string = Child;`)

	layered, err := ResolveLayers(base, 228, []LayerDifference{difference})
	require.NoError(t, err)

	parents := constructorsNamed(layered.Schema.Constructors, "parent")
	require.Len(t, parents, 1, "boxed child evolution does not version its parent")
	assert.Equal(t, 228, parents[0].Intervals[0].MinLayer)
	assert.Equal(t, 0, parents[0].Intervals[0].MaxLayer)

	var embedded []Constructor
	for _, declaration := range layered.Schema.Types {
		if declaration.Name == "Parent" {
			embedded = append(embedded, declaration.Constructors...)
		}
	}
	require.Len(t, embedded, 1)
}

func TestResolveLayersKeepsUniqueIntroducedIDsValidUpward(t *testing.T) {
	base := mustParseLayerSchema(t, 228, `---types---
result#00000001 = Result;`)
	introduced := mustParseDifference(t, 229, `---types---
later#00000002 = Later;
---functions---
later.call#00000003 = Result;`)
	removed := mustParseDifference(t, 230, `
// @tlrpc remove constructor later
// @tlrpc remove function later.call`)

	layered, err := ResolveLayers(base, 228, []LayerDifference{introduced, removed})
	require.NoError(t, err)
	constructors := constructorsNamed(layered.Schema.Constructors, "later")
	require.Len(t, constructors, 1)
	assert.Equal(t, 229, constructors[0].Intervals[0].MinLayer)
	assert.Equal(t, 229, constructors[0].Intervals[0].MaxLayer)
	methods := functionsNamed(layered.Schema.Functions, "later.call")
	require.Len(t, methods, 1)
	assert.Equal(t, 229, methods[0].Intervals[0].MinLayer)
	assert.Equal(t, 229, methods[0].Intervals[0].MaxLayer)
}

func TestResolveLayersStopsConcretePropagationAtStableUnion(t *testing.T) {
	base := mustParseLayerSchema(t, 228, `---types---
keyboardButton#00000001 text:string = KeyboardButton;
keyboardButtonURL#00000002 text:string url:string = KeyboardButton;
keyboardButtonRow#00000003 buttons:vector<KeyboardButton> = KeyboardButtonRow;
replyKeyboardMarkup#00000004 rows:vector<KeyboardButtonRow> = ReplyMarkup;
result#00000005 = Result;
---functions---
messages.send#00000010 markup:ReplyMarkup = Result;`)
	difference := mustParseDifference(t, 229, `
// @tlrpc remove constructor keyboardButtonURL
---types---
keyboardButton#00000001 text:string kind:int = KeyboardButton;
replyKeyboardMarkup#00000004 flags:# rows:vector<KeyboardButtonRow> force_reply:flags.0?true = ReplyMarkup;`)

	layered, err := ResolveLayers(base, 228, []LayerDifference{difference})
	require.NoError(t, err)

	var keyboardButton TypeDecl
	for _, declaration := range layered.Schema.Types {
		if declaration.Name == "KeyboardButton" {
			keyboardButton = declaration
			break
		}
	}
	assert.True(t, keyboardButton.IsUnion)
	assert.Zero(t, keyboardButton.VariantLayer)
	require.Len(t, keyboardButton.Constructors, 3)
	buttonVariants := constructorsNamed(keyboardButton.Constructors, "keyboardButton")
	require.Len(t, buttonVariants, 2)
	assert.Equal(t, 228, buttonVariants[0].Intervals[0].MaxLayer)
	assert.Equal(t, 229, buttonVariants[1].VariantLayer)
	assert.Equal(t, 229, buttonVariants[1].Intervals[0].MinLayer)
	removed := constructorsNamed(keyboardButton.Constructors, "keyboardButtonURL")
	require.Len(t, removed, 1)
	assert.Equal(t, 228, removed[0].Intervals[0].MaxLayer)

	rows := constructorsNamed(layered.Schema.Constructors, "keyboardButtonRow")
	require.Len(t, rows, 1, "a concrete parent that references a stable union remains shared")
	assert.Zero(t, rows[0].VariantLayer)
	assert.Zero(t, rows[0].Params[0].Type.Generic.VariantLayer)

	markups := constructorsNamed(layered.Schema.Constructors, "replyKeyboardMarkup")
	require.Len(t, markups, 2, "same-ID optional additions are distinct contracts")
	require.Len(t, markups[0].Params, 1)
	require.Len(t, markups[1].Params, 3)

	methods := functionsNamed(layered.Schema.Functions, "messages.send")
	require.Len(t, methods, 1, "a stable-union change must not duplicate an unchanged request")
	assert.Zero(t, methods[0].Params[0].Type.VariantLayer)
}

func TestResolveLayersTelegramVariantBudget(t *testing.T) {
	baseInput, err := os.ReadFile("../../testdata/schemas/telegram_layer_228.tl")
	require.NoError(t, err)
	differenceInput, err := os.ReadFile("../../testdata/schemas/telegram_layer_229.tl")
	require.NoError(t, err)

	baseline := strings.ReplaceAll(string(baseInput), "// @tlrpc variant-layer 172\n", "// @tlrpc variant-layer 172\n// @tlrpc accept-layers 228-229\n")
	baseline = strings.ReplaceAll(baseline, "// @tlrpc variant-layer 158\n", "// @tlrpc variant-layer 158\n// @tlrpc accept-layers 228-229\n")
	base, err := ParseBaselineSchema(baseline, 228, "telegram_layer_228.tl")
	require.NoError(t, err)
	difference, err := ParseLayerDifference(string(differenceInput), 229, "telegram_layer_229.tl")
	require.NoError(t, err)
	layered, err := ResolveLayers(base, 228, []LayerDifference{difference})
	require.NoError(t, err)

	var objectVariants []string
	for _, constructor := range layered.Schema.Constructors {
		if constructor.VariantLayer != 0 {
			objectVariants = append(objectVariants, fmt.Sprintf("%s@%d", constructor.Name, constructor.VariantLayer))
		}
	}
	sort.Strings(objectVariants)
	assert.Equal(t, []string{
		"channelFull@229",
		"chatAdminRights@229",
		"chatFull@229",
		"ephemeralMessage@229",
		"inputInvoiceStarGiftResale@229",
		"inputSendMessageRichMessageDraftAction@229",
		"keyboardButton@229",
		"messageActionStarGiftUnique@229",
		"pageBlockBlockquote@229",
		"pageBlockTable@229",
		"replyInlineMarkup@229",
		"replyKeyboardMarkup@229",
		"sendMessageRichMessageDraftAction@229",
		"sendMessageTextDraftAction@229",
		"updateEphemeralBotCallbackQuery@229",
	}, objectVariants)

	var requestVariants []string
	for _, function := range layered.Schema.Functions {
		if function.VariantLayer != 0 {
			requestVariants = append(requestVariants, fmt.Sprintf("%s@%d", function.Name, function.VariantLayer))
		}
	}
	sort.Strings(requestVariants)
	assert.Equal(t, []string{
		"auth.signUp@172",
		"ephemeral.deleteMessage@229",
		"ephemeral.editMessage@229",
		"ephemeral.sendMessage@229",
		"messages.forwardMessages@229",
		"updates.getDifference@158",
	}, requestVariants)

	for _, name := range []string{"KeyboardButton", "KeyboardButtonRow", "Message", "ReplyMarkup", "Updates"} {
		var declarations []TypeDecl
		for _, declaration := range layered.Schema.Types {
			if declaration.Name == name {
				declarations = append(declarations, declaration)
			}
		}
		require.Len(t, declarations, 1, "%s must remain one unsuffixed type", name)
		assert.Zero(t, declarations[0].VariantLayer, "%s acquired a transitive variant", name)
	}
	for _, name := range []string{"messages.editMessage", "messages.sendMessage", "messages.setTyping"} {
		methods := functionsNamed(layered.Schema.Functions, name)
		require.Len(t, methods, 1, "%s acquired a response/nested-only method variant", name)
		assert.Zero(t, methods[0].VariantLayer)
	}
}

func TestResolveLayersSeparatesRequestAndHandlerIdentity(t *testing.T) {
	base := mustParseLayerSchema(t, 228, `---types---
resultA#00000001 = ResultA;
resultB#00000002 = ResultB;
---functions---
test.call#00000010 value:int = ResultA;`)
	changedResult := mustParseDifference(t, 229, `---functions---
test.call#00000010 value:int = ResultB;`)

	layered, err := ResolveLayers(base, 228, []LayerDifference{changedResult})
	require.NoError(t, err)
	methods := functionsNamed(layered.Schema.Functions, "test.call")
	require.Len(t, methods, 2)
	assert.Zero(t, methods[0].RequestVariantLayer)
	assert.Zero(t, methods[1].RequestVariantLayer)
	assert.Zero(t, methods[0].VariantLayer)
	assert.Equal(t, 229, methods[1].VariantLayer)
	assert.Equal(t, []LayerInterval{{MinLayer: 228}}, methods[0].RequestIntervals)
	assert.Equal(t, methods[0].RequestIntervals, methods[1].RequestIntervals)
	assert.Equal(t, []LayerInterval{{MinLayer: 228, MaxLayer: 228}}, methods[0].Intervals)
	assert.Equal(t, []LayerInterval{{MinLayer: 229}}, methods[1].Intervals)
}

func TestResolveLayersUsesSemanticKeysAndReusesReappearingContract(t *testing.T) {
	base := mustParseLayerSchema(t, 228, `---types---
item#00000001 flags:# labels:flags.0?vector<string> = Item;`)
	removed := mustParseDifference(t, 229, `// @tlrpc remove constructor item`)
	reappeared := mustParseDifference(t, 230, `---types---
item#00000001 flags:# labels:flags.0?vector<string> = Item;`)

	layered, err := ResolveLayers(base, 228, []LayerDifference{removed, reappeared})
	require.NoError(t, err)
	items := constructorsNamed(layered.Schema.Constructors, "item")
	require.Len(t, items, 1)
	assert.Zero(t, items[0].VariantLayer)
	assert.Equal(t, []LayerInterval{{MinLayer: 228, MaxLayer: 228}, {MinLayer: 230}}, items[0].Intervals)
}

func TestResolveLayersRejectsRecursiveBareLayouts(t *testing.T) {
	base := mustParseLayerSchema(t, 228, `---types---
	alpha#1 beta:!Beta = Alpha;
	beta#2 alpha:!Alpha = Beta;`)
	_, err := ResolveLayers(base, 228, nil)
	require.ErrorContains(t, err, "unsupported recursive bare layout")
}

func TestResolveLayersValidatesHistoricalRangesAndSameIDOverlap(t *testing.T) {
	outside, err := ParseBaselineSchema(`---functions---
// @tlrpc variant-layer 172
// @tlrpc accept-layers 228-230
a#1 old:int = X;
a#2 current:int = X;`, 228, "baseline.tl")
	require.NoError(t, err)
	_, err = ResolveLayers(outside, 228, []LayerDifference{mustParseDifference(t, 229, "")})
	require.ErrorContains(t, err, "outside supplied history")

	overlap, err := ParseBaselineSchema(`---functions---
// @tlrpc variant-layer 172
// @tlrpc accept-layers 228-228
a#1 old:int = X;
a#1 current:string = X;`, 228, "baseline.tl")
	require.NoError(t, err)
	_, err = ResolveLayers(overlap, 228, nil)
	require.ErrorContains(t, err, "function ID 0x00000001 has overlapping contracts")
}

func TestResolveSelectedLayerFiltersSameIDHistoryBeforeApplyingDifferences(t *testing.T) {
	base, err := ParseBaselineSchema(`---functions---
// @tlrpc variant-layer 227
// @tlrpc accept-layers 229-229
test.call#00000001 value:int = Result;
test.call#00000001 value:string = Result;`, 228, "baseline.tl")
	require.NoError(t, err)
	difference := mustParseDifference(t, 229, `---functions---
test.call#00000002 value:string = Result;`)

	selected, err := ResolveSelectedLayer(base, 228, 229, []LayerDifference{difference})
	require.NoError(t, err)
	methods := functionsNamed(selected.Functions, "test.call")
	require.Len(t, methods, 2)
	assert.Equal(t, uint32(2), methods[0].ID)
	assert.Zero(t, methods[0].VariantLayer)
	assert.Equal(t, uint32(1), methods[1].ID)
	assert.Equal(t, 227, methods[1].VariantLayer)
}

func TestResolveSelectedLayerValidatesFullSuppliedHistory(t *testing.T) {
	base, err := ParseBaselineSchema(`---functions---
// @tlrpc variant-layer 227
// @tlrpc accept-layers 229-230
test.call#00000001 value:int = Result;
test.call#00000002 value:string = Result;`, 228, "baseline.tl")
	require.NoError(t, err)

	_, err = ResolveSelectedLayer(base, 228, 229, []LayerDifference{mustParseDifference(t, 229, "")})
	require.ErrorContains(t, err, "outside supplied history 228-229")
}

func constructorsNamed(constructors []Constructor, name string) []Constructor {
	var matches []Constructor
	for _, constructor := range constructors {
		if constructor.Name == name {
			matches = append(matches, constructor)
		}
	}
	return matches
}

func functionsNamed(functions []FuncDecl, name string) []FuncDecl {
	var matches []FuncDecl
	for _, function := range functions {
		if function.Name == name {
			matches = append(matches, function)
		}
	}
	return matches
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

func TestHistoricalBareContractUsesItsAcceptedSnapshot(t *testing.T) {
	base, err := ParseBaselineSchema(`---types---
child#1 value:int = Child;
result#2 = Result;
---functions---
// @tlrpc variant-layer 172
// @tlrpc accept-layers 228-228
call#3 child:!Child = Result;
call#4 = Result;`, 228, "base.tl")
	require.NoError(t, err)
	delta := mustParseDifference(t, 229, "---types---\nchild#1 value:long = Child;")
	resolved, err := ResolveLayers(base, 228, []LayerDifference{delta})
	require.NoError(t, err)
	for _, method := range resolved.Schema.Functions {
		if method.VariantLayer == 172 {
			assert.Zero(t, method.Params[0].Type.VariantLayer)
		}
	}
	base.Functions[0].AcceptIntervals = []LayerInterval{{MinLayer: 228, MaxLayer: 229}}
	_, err = ResolveLayers(base, 228, []LayerDifference{delta})
	require.ErrorContains(t, err, "bare contract changes within accepted layers")
	_, err = ResolveSelectedLayer(base, 228, 228, []LayerDifference{delta})
	require.ErrorContains(t, err, "bare contract changes within accepted layers")
}

func TestBareFamilyConstructorRenameRejectsGeneration(t *testing.T) {
	base := mustParseLayerSchema(t, 228, "---types---\nfirst#1 value:int = Child;\nparent#2 child:!Child = Parent;")
	delta := mustParseDifference(t, 229, "---types---\n// @tlrpc remove constructor first\nsecond#3 value:long = Child;")
	_, err := ResolveLayers(base, 228, []LayerDifference{delta})
	require.ErrorContains(t, err, "replacing constructor name first with second is unsupported")
}
