package generator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r6m/tlrpc/internal/naming"
	"github.com/r6m/tlrpc/internal/parser"
)

func TestProjectionGeneratorGeneratedRuntime(t *testing.T) {
	base, err := parser.NewParser(`---types---
leafText#00000001 text:string = Leaf;
leafBytes#00000002 data:bytes = Leaf;
node#00000003 flags:# label:string next:flags.0?Node children:Vector<Node> = Node;
card#00000004 title:string leaf:Leaf tags:Vector<string> payload:bytes = Card;
policy#00000005 flags:# name:string = Policy;
result#00000006 = Result;
actionText#00000011 text:string = Action;
actionGone#00000012 = Action;
envelope#00000013 action:Action = Envelope;
rights#00000015 flags:# name:string = Rights;
holder#00000016 rights:Rights = Holder;
bareChild#00000031 value:int = BareChild;
bareHolder#00000032 flags:# child:!BareChild optional:flags.0?!BareChild children:Vector<!BareChild> boxed:BareChild = BareHolder;
---functions---
work.run#00000010 card:Card = Result;
work.bare#00000033 flags:# child:!BareChild optional:flags.0?!BareChild children:Vector<!BareChild> = Result;
`).ParseWithLayer(228)
	if err != nil {
		t.Fatalf("parse base schema: %v", err)
	}
	difference, err := parser.ParseLayerDifference(`---types---
card#00000007 title:string leaf:Leaf tags:Vector<string> payload:bytes required:string = Card;
policy#00000005 flags:# name:string danger:flags.0?true = Policy;
// @tlrpc remove constructor actionGone
actionText#00000014 text:string extra:string = Action;
rights#00000015 flags:# name:string welcome:flags.0?true = Rights;
bareChild#00000031 value:long = BareChild;
`, 229, "layer229.tl")
	if err != nil {
		t.Fatalf("parse layer difference: %v", err)
	}
	reappearance, err := parser.ParseLayerDifference("---types---\nactionGone#00000012 = Action;", 230, "layer230.tl")
	if err != nil {
		t.Fatalf("parse reappearance: %v", err)
	}
	layered, err := parser.ResolveLayers(base, 228, []parser.LayerDifference{difference, reappearance})
	if err != nil {
		t.Fatalf("resolve layered schema: %v", err)
	}
	moduleDir := t.TempDir()
	generatedDir := filepath.Join(moduleDir, "projectionmini")
	if err := generateProjectionTestPackage(generatedDir, layered.Schema); err != nil {
		t.Fatalf("generate projection package: %v", err)
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	goMod := fmt.Sprintf(`module example.com/projection-test

go 1.25

require github.com/r6m/tlrpc v0.0.0

replace github.com/r6m/tlrpc => %s
`, filepath.ToSlash(repoRoot))
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(generatedDir, "projection_runtime_test.go"), []byte(projectionRuntimeTest), 0o600); err != nil {
		t.Fatalf("write generated runtime test: %v", err)
	}

	cmd := exec.Command("go", "test", "-count=1", "./projectionmini")
	cmd.Dir = moduleDir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated projection package failed:\n%s\n%v", output, err)
	}

	vet := exec.Command("go", "vet", "./projectionmini")
	vet.Dir = moduleDir
	vet.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	output, err = vet.CombinedOutput()
	if err != nil {
		t.Fatalf("generated projection package failed vet:\n%s\n%v", output, err)
	}
}

func TestProjectionGeneratorValidation(t *testing.T) {
	namer := naming.NewNamer()
	var output strings.Builder
	if err := NewProjectionGenerator(namer, &output).Generate(nil); err == nil {
		t.Fatal("expected nil schema error")
	}

	schema := parser.NewSchema(229)
	schema.BaseLayer = 228
	schema.IsLayered = true
	first := parser.Constructor{Name: "item", ID: 1, ResultType: parser.NewTypeRef("Item"), Intervals: []parser.LayerInterval{{MinLayer: 228, MaxLayer: 229}}}
	second := parser.Constructor{Name: "item", ID: 2, ResultType: parser.NewTypeRef("Item"), VariantLayer: 229, Intervals: []parser.LayerInterval{{MinLayer: 229, MaxLayer: 229}}}
	schema.AddType(parser.TypeDecl{Name: "Item", Constructors: []parser.Constructor{first}, VariantLayer: 0})
	schema.AddType(parser.TypeDecl{Name: "Item", Constructors: []parser.Constructor{second}, VariantLayer: 229})
	if err := NewProjectionGenerator(namer, &output).Generate(schema); err == nil || !strings.Contains(err.Error(), "overlapping output ranges") {
		t.Fatalf("expected overlapping range error, got %v", err)
	}
}

func generateProjectionTestPackage(outDir string, schema *parser.Schema) error {
	writer := NewFileWriter(outDir, "projectionmini", "projection.tl", schema.Layer)
	namer := naming.NewNamer()
	typesOut := writer.NewFile("types.go")
	interfacesOut := writer.NewFile("interfaces.go")
	typesGenerator := NewTypeGenerator(namer, typesOut, schema)
	interfacesGenerator := NewTypeGenerator(namer, interfacesOut, schema)
	for i := range schema.Types {
		if err := typesGenerator.GenerateType(&schema.Types[i]); err != nil {
			return err
		}
		if err := interfacesGenerator.GenerateInterface(&schema.Types[i]); err != nil {
			return err
		}
	}
	requests := NewServiceGenerator(namer, schema, writer.NewFile("requests.go"))
	if err := requests.GenerateRequests(schema.Functions); err != nil {
		return err
	}
	if err := NewCodecGenerator(namer, writer.NewFile("codec.go")).Generate(schema); err != nil {
		return err
	}
	if err := GenerateBaseAliases(writer.NewFile("base_aliases.go")); err != nil {
		return err
	}
	if err := NewProjectionGenerator(namer, writer.NewFile("projection.go")).Generate(schema); err != nil {
		return err
	}
	return writer.WriteAll()
}

const projectionRuntimeTest = `package projectionmini

import (
	"bytes"
	"encoding/hex"
	"io"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/mtproto"
)

type unknownObject struct{}

func (*unknownObject) ConstructorID() uint32 { return 0xffffffff }

func TestRecursiveProjectionClonesAndSelectsVariant(t *testing.T) {
	source := &CardLayer229{
		Title: "title",
		Leaf: &LeafBytes{Data: []byte{1, 2, 3}},
		Tags: []string{"one", "two"},
		Payload: []byte{4, 5},
		Required: "semantic",
	}
	if _, err := ProjectTLObject(source, 228, nil); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("expected nonzero semantic-loss error, got %v", err)
	}
	source.Required = ""

	var paths [][]string
	hook := func(path []string, object tlrpc.TLObject, targetLayer int) (tlrpc.TLObject, bool, error) {
		paths = append(paths, append([]string(nil), path...))
		return nil, false, nil
	}
	projectedObject, err := ProjectTLObject(source, 228, hook)
	if err != nil {
		t.Fatalf("project card: %v", err)
	}
	projected, ok := projectedObject.(*Card)
	if !ok {
		t.Fatalf("projected type = %T, want *Card", projectedObject)
	}
	if projected == nil || projected.Leaf == source.Leaf {
		t.Fatal("nested object was not independently cloned")
	}
	leaf, ok := projected.Leaf.(*LeafBytes)
	if !ok {
		t.Fatalf("projected leaf = %T", projected.Leaf)
	}
	sourceLeaf := source.Leaf.(*LeafBytes)
	if &leaf.Data[0] == &sourceLeaf.Data[0] || &projected.Tags[0] == &source.Tags[0] || &projected.Payload[0] == &source.Payload[0] {
		t.Fatal("slice storage was shared with source")
	}
	leaf.Data[0], projected.Tags[0], projected.Payload[0] = 9, "changed", 9
	if sourceLeaf.Data[0] != 1 || source.Tags[0] != "one" || source.Payload[0] != 4 {
		t.Fatal("mutating projection changed source")
	}
	wantPaths := [][]string{{"card"}, {"card", "leaf", "leafBytes"}}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("hook paths = %#v, want %#v", paths, wantPaths)
	}
}

func TestSameIDVariantHookRunsBeforeLossCheck(t *testing.T) {
	source := &PolicyLayer229{Name: "policy", Danger: true}
	if _, err := ProjectTLObject(source, 228, nil); err == nil || !strings.Contains(err.Error(), "nonzero unsupported field") {
		t.Fatalf("expected unsupported semantic field error, got %v", err)
	}
	calls := 0
	replacement := &Policy{Name: "safe"}
	projectedObject, err := ProjectTLObject(source, 228, func(path []string, object tlrpc.TLObject, targetLayer int) (tlrpc.TLObject, bool, error) {
		calls++
		if !reflect.DeepEqual(path, []string{"policy"}) {
			t.Fatalf("hook path = %#v", path)
		}
		return replacement, true, nil
	})
	if err != nil {
		t.Fatalf("project policy with hook: %v", err)
	}
	if calls != 1 {
		t.Fatalf("hook calls = %d, want 1", calls)
	}
	projected := projectedObject.(*Policy)
	if projected == replacement {
		t.Fatal("handled hook replacement was not independently cloned")
	}
	if projected.Name != "safe" {
		t.Fatalf("unexpected projected policy: %#v", projected)
	}

	zero := &Policy{Name: "zero"}
	zeroProjected, err := ProjectTLObject(zero, 0, nil)
	if err != nil {
		t.Fatalf("project zero optional field: %v", err)
	}
	if zeroProjected == zero {
		t.Fatal("projection returned source pointer")
	}
}

func TestRequiredTargetFieldNeedsHook(t *testing.T) {
	if _, err := ProjectTLObject(&Card{Title: "malformed"}, 228, nil); err == nil || !strings.Contains(err.Error(), "required target field is missing") {
		t.Fatalf("expected nil required union field error, got %v", err)
	}
	source := &Card{Title: "old", Leaf: &LeafText{Text: "leaf"}}
	if _, err := ProjectTLObject(source, 229, nil); err == nil || !strings.Contains(err.Error(), "required target field is missing") {
		t.Fatalf("expected missing required field error, got %v", err)
	}
	projectedObject, err := ProjectTLObject(source, 229, func(_ []string, object tlrpc.TLObject, _ int) (tlrpc.TLObject, bool, error) {
		if value, ok := object.(*Card); ok {
			return &CardLayer229{Title: value.Title, Leaf: value.Leaf, Required: "provided"}, true, nil
		}
		return nil, false, nil
	})
	if err != nil {
		t.Fatalf("project required field with hook: %v", err)
	}
	if projectedObject.(*CardLayer229).Required != "provided" {
		t.Fatalf("hook replacement was not projected: %#v", projectedObject)
	}
}

func TestChangedConstructorProjectsThroughStableUnion(t *testing.T) {
	source := &Envelope{Action: &ActionTextLayer229{Text: "text", Extra: "semantic"}}
	if _, err := ProjectTLObject(source, 228, nil); err == nil || !strings.Contains(err.Error(), "nonzero unsupported field") {
		t.Fatalf("expected nested semantic-loss error, got %v", err)
	}
	var gotPath []string
	replacement := &ActionText{Text: "mapped"}
	projectedObject, err := ProjectTLObject(source, 228, func(path []string, object tlrpc.TLObject, _ int) (tlrpc.TLObject, bool, error) {
		if _, ok := object.(*ActionTextLayer229); ok {
			gotPath = append([]string(nil), path...)
			return replacement, true, nil
		}
		return nil, false, nil
	})
	if err != nil {
		t.Fatalf("project stable union: %v", err)
	}
	projected, ok := projectedObject.(*Envelope)
	if !ok {
		t.Fatalf("projected parent = %T, want stable *Envelope", projectedObject)
	}
	projectedAction, ok := projected.Action.(*ActionText)
	if !ok || projectedAction == replacement || projectedAction.Text != "mapped" {
		t.Fatalf("nested handled replacement was not cloned: %#v", projected.Action)
	}
	wantPath := []string{"envelope", "action", "actionText"}
	if !reflect.DeepEqual(gotPath, wantPath) {
		t.Fatalf("nested hook path = %#v, want %#v", gotPath, wantPath)
	}

	if _, err := ProjectTLObject(&ActionGone{}, 229, nil); err == nil || !strings.Contains(err.Error(), "unavailable at output layer") {
		t.Fatalf("expected removed union member rejection, got %v", err)
	}
	if _, err := ProjectTLObject(&Policy{}, 229, func(_ []string, _ tlrpc.TLObject, _ int) (tlrpc.TLObject, bool, error) {
		return &ActionGone{}, true, nil
	}); err == nil || !strings.Contains(err.Error(), "unavailable at output layer") {
		t.Fatalf("expected unavailable hook replacement rejection, got %v", err)
	}
	for _, layer := range []int{228, 230} {
		projected, err := ProjectTLObject(&ActionGone{}, layer, nil)
		if err != nil {
			t.Fatalf("project recurring member at layer %d: %v", layer, err)
		}
		if _, ok := projected.(*ActionGone); !ok {
			t.Fatalf("reappearing contract changed concrete type: %T", projected)
		}
	}
}

func TestHookReplacementIsRecursivelyValidated(t *testing.T) {
	if _, err := ProjectTLObject(&Policy{}, 228, func(_ []string, object tlrpc.TLObject, _ int) (tlrpc.TLObject, bool, error) {
		if _, ok := object.(*Policy); ok {
			return &Holder{Rights: &RightsLayer229{Name: "admin", Welcome: true}}, true, nil
		}
		return nil, false, nil
	}); err == nil || !strings.Contains(err.Error(), "nonzero unsupported field") {
		t.Fatalf("expected nested replacement semantic-loss error, got %v", err)
	}

	if _, err := ProjectTLObject(&Policy{}, 228, func(_ []string, object tlrpc.TLObject, _ int) (tlrpc.TLObject, bool, error) {
		if _, ok := object.(*Policy); ok {
			return &unknownObject{}, true, nil
		}
		return nil, false, nil
	}); err == nil || !strings.Contains(err.Error(), "unsupported source type") {
		t.Fatalf("expected unknown replacement rejection, got %v", err)
	}

	if _, err := ProjectTLObject(&Policy{}, 228, func(_ []string, object tlrpc.TLObject, _ int) (tlrpc.TLObject, bool, error) {
		if _, ok := object.(*Policy); ok {
			return &WorkRunRequest{}, true, nil
		}
		return nil, false, nil
	}); err == nil || !strings.Contains(err.Error(), "request input") {
		t.Fatalf("expected request replacement rejection, got %v", err)
	}
}

func TestUnknownRequestsErrorsAndDepthBound(t *testing.T) {
	if _, err := ProjectTLObject(&unknownObject{}, 228, nil); err == nil || !strings.Contains(err.Error(), "unsupported source type") {
		t.Fatalf("expected unsupported source error, got %v", err)
	}
	unknownCalls := 0
	projected, err := ProjectTLObject(&unknownObject{}, 228, func(_ []string, object tlrpc.TLObject, _ int) (tlrpc.TLObject, bool, error) {
		unknownCalls++
		return &Policy{Name: "known"}, true, nil
	})
	if err != nil || projected.(*Policy).Name != "known" || unknownCalls != 1 {
		t.Fatalf("unknown hook projection = %#v, calls %d, err %v", projected, unknownCalls, err)
	}

	requestHookCalls := 0
	if _, err := ProjectTLObject(&WorkRunRequest{}, 228, func(_ []string, _ tlrpc.TLObject, _ int) (tlrpc.TLObject, bool, error) {
		requestHookCalls++
		return &Policy{}, true, nil
	}); err == nil || !strings.Contains(err.Error(), "request input") {
		t.Fatalf("expected request rejection, got %v", err)
	}
	if requestHookCalls != 0 {
		t.Fatalf("request hook calls = %d, want 0", requestHookCalls)
	}
	if _, err := ProjectTLObject(&Policy{}, 228, func(_ []string, _ tlrpc.TLObject, _ int) (tlrpc.TLObject, bool, error) {
		return nil, true, nil
	}); err == nil || !strings.Contains(err.Error(), "nil replacement") {
		t.Fatalf("expected nil hook replacement error, got %v", err)
	}

	var childPath []string
	tree := &Node{Label: "root", Children: []NodeType{&Node{Label: "child"}}}
	projectedTreeObject, err := ProjectTLObject(tree, 228, func(path []string, object tlrpc.TLObject, _ int) (tlrpc.TLObject, bool, error) {
		if value, ok := object.(*Node); ok && value.Label == "root" {
			path[0] = "mutated-copy"
		}
		if value, ok := object.(*Node); ok && value.Label == "child" {
			childPath = append([]string(nil), path...)
		}
		return nil, false, nil
	})
	if err != nil {
		t.Fatalf("project vector tree: %v", err)
	}
	projectedTree := projectedTreeObject.(*Node)
	if projectedTree == tree || projectedTree.Children[0] == tree.Children[0] {
		t.Fatal("recursive vector objects were not independently cloned")
	}
	wantChildPath := []string{"node", "children", "[0]", "node"}
	if !reflect.DeepEqual(childPath, wantChildPath) {
		t.Fatalf("vector child path = %#v, want %#v", childPath, wantChildPath)
	}

	cycle := &Node{Label: "cycle"}
	cycle.Next = cycle
	if _, err := ProjectTLObject(cycle, 228, nil); err == nil || !strings.Contains(err.Error(), "maximum projection depth") {
		t.Fatalf("expected depth error, got %v", err)
	}

	for _, layer := range []int{227, 231} {
		if _, err := ProjectTLObject(&Policy{}, layer, nil); err == nil || !strings.Contains(err.Error(), "unsupported target layer") {
			t.Fatalf("layer %d: expected range error, got %v", layer, err)
		}
	}
	wantErr := errors.New("policy denied")
	if _, err := ProjectTLObject(&Policy{}, 228, func(_ []string, _ tlrpc.TLObject, _ int) (tlrpc.TLObject, bool, error) {
		return nil, false, wantErr
	}); err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("expected hook error propagation, got %v", err)
	}
}

func TestBareCodecsPreserveWireTagsAcrossLayers(t *testing.T) {
	type codec interface { SerializeTL(io.Writer) error; DeserializeTL(io.Reader) error }
	cases := []struct { layer int; source, target codec; wire string }{
		{228, &BareHolder{Child: BareChild{Value:7}, Optional:&BareChild{Value:8}, Children:[]*BareChild{{Value:9}}, Boxed:&BareChild{Value:10}}, &BareHolder{}, "3200000001000000070000000800000015c4b51c0100000009000000310000000a000000"},
		{229, &BareHolderLayer229{Child: BareChildLayer229{Value:7}, Optional:&BareChildLayer229{Value:8}, Children:[]*BareChildLayer229{{Value:9}}, Boxed:&BareChildLayer229{Value:10}}, &BareHolderLayer229{}, "32000000010000000700000000000000080000000000000015c4b51c010000000900000000000000310000000a00000000000000"},
		{228, &WorkBareRequest{Child:BareChild{Value:7}, Optional:&BareChild{Value:8}, Children:[]*BareChild{{Value:9}}}, &WorkBareRequest{}, "3300000001000000070000000800000015c4b51c0100000009000000"},
		{229, &WorkBareRequestLayer229{Child:BareChildLayer229{Value:7}, Optional:&BareChildLayer229{Value:8}, Children:[]*BareChildLayer229{{Value:9}}}, &WorkBareRequestLayer229{}, "33000000010000000700000000000000080000000000000015c4b51c010000000900000000000000"},
	}
	for _, test := range cases {
		var output bytes.Buffer
		if err := test.source.SerializeTL(mtproto.WithLayerWriter(&output, test.layer)); err != nil { t.Fatal(err) }
		want, err := hex.DecodeString(test.wire)
		if err != nil { t.Fatal(err) }
		if !bytes.Equal(output.Bytes(), want) { t.Fatalf("%T: bytes %x, want %x", test.source, output.Bytes(), want) }
		if err := test.target.DeserializeTL(mtproto.WithLayerReader(bytes.NewReader(want), test.layer)); err != nil { t.Fatalf("%T: %v", test.target, err) }
		if !reflect.DeepEqual(test.source, test.target) { t.Fatalf("%T: roundtrip changed values", test.source) }
	}
}
`
