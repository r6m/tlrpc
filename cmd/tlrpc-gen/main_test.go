package main

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLayer(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{name: "unspecified", want: 0},
		{name: "layer", input: "228", want: 228},
		{name: "trim whitespace", input: " 228 ", want: 228},
		{name: "multiple layers", input: "217,228", wantErr: true},
		{name: "invalid", input: "latest", wantErr: true},
		{name: "zero", input: "0", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLayer(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseLayer() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("parseLayer() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseLayerDiff(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    layerDiffSpec
		wantErr bool
	}{
		{name: "layer and path", input: "101:schemas/layer-101.tl", want: layerDiffSpec{Layer: 101, Path: "schemas/layer-101.tl"}},
		{name: "colon in path", input: "102:schemas/archive:layer-102.tl", want: layerDiffSpec{Layer: 102, Path: "schemas/archive:layer-102.tl"}},
		{name: "missing separator", input: "101", wantErr: true},
		{name: "missing layer", input: ":layer.tl", wantErr: true},
		{name: "invalid layer", input: "latest:layer.tl", wantErr: true},
		{name: "zero layer", input: "0:layer.tl", wantErr: true},
		{name: "missing path", input: "101:", wantErr: true},
		{name: "blank path", input: "101:   ", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLayerDiff(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseLayerDiff() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("parseLayerDiff() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestLayerDiffFlagsPreserveSuppliedOrder(t *testing.T) {
	var flags layerDiffFlags
	for _, value := range []string{"103:third.tl", "101:first.tl", "102:second.tl"} {
		if err := flags.Set(value); err != nil {
			t.Fatalf("Set(%q): %v", value, err)
		}
	}

	want := []layerDiffSpec{
		{Layer: 103, Path: "third.tl"},
		{Layer: 101, Path: "first.tl"},
		{Layer: 102, Path: "second.tl"},
	}
	if fmt.Sprint(flags) != fmt.Sprint(want) {
		t.Fatalf("flags = %#v, want %#v", flags, want)
	}
}

func TestRun_LayerDiffRequiresBaseAndTargetLayers(t *testing.T) {
	basePath := writeTestFile(t, "base.tl", testBaseSchema)
	diffPath := writeTestFile(t, "layer-101.tl", "---types---\n")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "base layer",
			args: []string{"--schema=" + basePath, "--layer=101", "--layer-diff=101:" + diffPath},
			want: "--base-layer must be a positive integer",
		},
		{
			name: "target layer",
			args: []string{"--schema=" + basePath, "--base-layer=100", "--layer-diff=101:" + diffPath},
			want: "--layer must be a positive integer",
		},
		{
			name: "malformed difference",
			args: []string{"--schema=" + basePath, "--base-layer=100", "--layer=101", "--layer-diff=101"},
			want: "expected <layer>:<path>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr strings.Builder
			if code := run(tt.args, io.Discard, &stderr); code != 3 {
				t.Fatalf("run returned %d, want 3; stderr: %s", code, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("stderr %q does not contain %q", stderr.String(), tt.want)
			}
		})
	}
}

func TestRunRejectsRemovedCompatibilitySchemaFlag(t *testing.T) {
	var stderr strings.Builder
	code := run([]string{"--compat-schema=historical.tl"}, io.Discard, &stderr)
	if code != 3 {
		t.Fatalf("run returned %d, want 3; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -compat-schema") {
		t.Fatalf("stderr %q does not reject removed --compat-schema", stderr.String())
	}
}

func TestRun_ResolvesOrderedLayerDifferencesAndRecordsSelectedProvenance(t *testing.T) {
	baseData := []byte(testBaseSchema)
	basePath := writeTestFile(t, "base.tl", string(baseData))
	diff101 := []byte("---types---\nitem#101 value:long = Item;\n---functions---\n")
	diff102 := []byte("---types---\nextra#102 label:string = Extra;\n---functions---\n")
	diff103 := []byte("---types---\nfuture#103 enabled:Bool = Future;\n---functions---\n")
	path101 := writeTestFile(t, "layer-101.tl", string(diff101))
	path102 := writeTestFile(t, "layer-102.tl", string(diff102))
	path103 := writeTestFile(t, "layer-103.tl", string(diff103))

	outDir := t.TempDir()
	var stderr strings.Builder
	code := run([]string{
		"--schema=" + basePath,
		"--base-layer=100",
		"--layer=102",
		"--layer-diff=101:" + path101,
		"--layer-diff=102:" + path102,
		"--layer-diff=103:" + path103,
		"--out=" + outDir,
		"--package=layered",
	}, io.Discard, &stderr)
	if code != 0 {
		t.Fatalf("run returned %d: %s", code, stderr.String())
	}

	wantSource := "// Source: base.tl + layer-101.tl + layer-102.tl\n"
	wantDigest := framedDigest(baseData, diff101, diff102)
	constants, err := os.ReadFile(filepath.Join(outDir, "constants.go"))
	if err != nil {
		t.Fatalf("read constants.go: %v", err)
	}
	generated := string(constants)
	if !strings.Contains(generated, wantSource) {
		t.Fatalf("generated provenance missing %q:\n%s", wantSource, generated)
	}
	if !strings.Contains(generated, "// Schema SHA-256: "+wantDigest+"\n") {
		t.Fatalf("generated provenance missing framed digest %s", wantDigest)
	}
	if strings.Contains(generated, "layer-103.tl") {
		t.Fatal("generated provenance includes a difference newer than the selected target")
	}
	if !strings.Contains(generated, "const SchemaLayer = 102") {
		t.Fatal("generated output does not identify selected target layer 102")
	}
}

func TestRun_PropagatesLayerDifferenceOrderToResolver(t *testing.T) {
	basePath := writeTestFile(t, "base.tl", testBaseSchema)
	diff101 := writeTestFile(t, "layer-101.tl", "---types---\nfirst#101 = First;\n---functions---\n")
	diff102 := writeTestFile(t, "layer-102.tl", "---types---\nsecond#102 = Second;\n---functions---\n")
	var stderr strings.Builder
	code := run([]string{
		"--schema=" + basePath,
		"--base-layer=100",
		"--layer=102",
		"--layer-diff=102:" + diff102,
		"--layer-diff=101:" + diff101,
		"--out=" + t.TempDir(),
	}, io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("run returned %d, want 1; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "difference layers must be strictly increasing") {
		t.Fatalf("stderr does not show supplied order reached resolver: %s", stderr.String())
	}
}

func TestRun_GeneratesResolvedLayerEndToEnd(t *testing.T) {
	basePath := writeTestFile(t, "base.tl", testBaseSchema+`---types---
legacy#300 = Legacy;

---functions---
legacy.get#301 = Legacy;
`)
	diff101 := writeTestFile(t, "layer-101.tl", `---types---
item#101 value:long = Item;

---functions---
items.get#201 id:int = Item;
`)
	diff102 := writeTestFile(t, "layer-102.tl", `---types---
// @tlrpc remove constructor legacy
extra#102 label:string = Extra;

---functions---
// @tlrpc remove function legacy.get
items.list#202 = Extra;
`)
	moduleDir := t.TempDir()
	outDir := filepath.Join(moduleDir, "gen")
	var stderr strings.Builder
	code := run([]string{
		"--schema=" + basePath,
		"--base-layer=100",
		"--layer=102",
		"--layer-diff=101:" + diff101,
		"--layer-diff=102:" + diff102,
		"--out=" + outDir,
		"--package=layered",
	}, io.Discard, &stderr)
	if code != 0 {
		t.Fatalf("run returned %d: %s", code, stderr.String())
	}

	constants, err := os.ReadFile(filepath.Join(outDir, "constants.go"))
	if err != nil {
		t.Fatalf("read constants.go: %v", err)
	}
	for _, want := range []string{
		"const SchemaLayer = 102",
		"const ItemConstructorID uint32 = 0x00000101",
		"const ExtraConstructorID uint32 = 0x00000102",
	} {
		if !strings.Contains(string(constants), want) {
			t.Fatalf("constants.go missing %q:\n%s", want, constants)
		}
	}

	requests, err := os.ReadFile(filepath.Join(outDir, "requests.go"))
	if err != nil {
		t.Fatalf("read requests.go: %v", err)
	}
	for _, want := range []string{"type ItemsGetRequest struct", "type ItemsListRequest struct"} {
		if !strings.Contains(string(requests), want) {
			t.Fatalf("requests.go missing %q", want)
		}
	}
	if strings.Contains(string(constants), "LegacyConstructorID") || strings.Contains(string(requests), "LegacyGetRequest") {
		t.Fatal("generated output retained declarations removed by the selected layer")
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve module root: %v", err)
	}
	goMod := fmt.Sprintf("module example.com/layered-generation\n\ngo 1.25\n\nrequire github.com/r6m/tlrpc v0.0.0\n\nreplace github.com/r6m/tlrpc => %s\n", root)
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatalf("write generated module: %v", err)
	}
	command := exec.Command("go", "test", "-mod=mod", "-p=1", "./gen")
	command.Dir = moduleDir
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("compile generated layered package: %v\n%s", err, output)
	}
}

func writeTestFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func framedDigest(inputs ...[]byte) string {
	digest := sha256.New()
	for _, input := range inputs {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(input)))
		_, _ = digest.Write(size[:])
		_, _ = digest.Write(input)
	}
	return fmt.Sprintf("%x", digest.Sum(nil))
}

const testBaseSchema = `---types---
item#100 value:int = Item;

---functions---
items.get#200 = Item;
`

func TestRun_GeneratedHeadersContainExactSchemaProvenance(t *testing.T) {
	schemaPath := filepath.Join("..", "..", "testdata", "schemas", "simple.tl")
	schemaInput, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	wantDigest := fmt.Sprintf("%x", sha256.Sum256(schemaInput))
	wantHeader := "// Source: simple.tl\n// Schema SHA-256: " + wantDigest + "\n// Layer: 228\n"
	outDir := t.TempDir()

	if code := run([]string{
		"--schema=" + schemaPath,
		"--out=" + outDir,
		"--package=telegram",
		"--layer=228",
	}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("run returned exit code %d", code)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("read output directory: %v", err)
	}
	generated := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		generated++
		data, err := os.ReadFile(filepath.Join(outDir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		if !strings.Contains(string(data), wantHeader) {
			t.Fatalf("%s missing exact schema provenance header", entry.Name())
		}
		if entry.Name() == "constants.go" && !strings.Contains(string(data), "const SchemaLayer = 228") {
			t.Fatalf("constants.go missing generated schema layer constant")
		}
	}
	if generated == 0 {
		t.Fatal("expected generated Go files")
	}
	if _, err := os.Stat(filepath.Join(outDir, "projection.go")); !os.IsNotExist(err) {
		t.Fatalf("single-layer generation wrote projection.go: %v", err)
	}
}

func TestRun_GeneratesMultiLayerPackageRoundTrip(t *testing.T) {
	basePath := writeTestFile(t, "base-228.tl", `---types---
child#00000001 flags:# active:flags.0?true = Child;
legacyNested#00000002 value:int = Nested;
container#00000003 child:Child nested:Nested = Container;
result#00000004 = Result;
matrix#00000006 values:Vector<Vector<int>> = Matrix;
---functions---
messages.forward#00000020 flags:# silent:flags.0?true container:Container = Result;
// @tlrpc variant-layer 172
// @tlrpc accept-layers 228-229
messages.forward#00000021 container:Container = Result;
auth.check#00000030 value:int = Result;
test.matrix#00000040 values:Vector<Vector<int>> = Matrix;
`)
	deltaPath := writeTestFile(t, "delta-229.tl", `
// @tlrpc remove constructor legacyNested
---types---
child#00000001 flags:# active:flags.0?true label:flags.1?string = Child;
currentNested#00000005 text:string = Nested;
---functions---
messages.forward#00000020 flags:# silent:flags.0?true from_ephemeral:flags.1?true container:Container = Result;
auth.check#00000031 value:string = Result;
messages.newMethod#00000022 = Result;
`)
	moduleDir := t.TempDir()
	outDir := filepath.Join(moduleDir, "gen")
	var stderr strings.Builder
	if code := run([]string{
		"--schema=" + basePath,
		"--base-layer=228",
		"--layers=228,229",
		"--layer-diff=229:" + deltaPath,
		"--out=" + outDir,
		"--package=gen",
	}, io.Discard, &stderr); code != 0 {
		t.Fatalf("run returned %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(outDir, "projection.go")); err != nil {
		t.Fatalf("layered generation did not write projection.go: %v", err)
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goMod := fmt.Sprintf("module example.com/multilayer-generation\n\ngo 1.25\n\nrequire github.com/r6m/tlrpc v0.0.0\n\nreplace github.com/r6m/tlrpc => %s\n", root)
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "roundtrip_test.go"), []byte(multiLayerRoundTripTest), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", "-mod=mod", "-p=1", "./...")
	command.Dir = moduleDir
	command.Env = append(os.Environ(), "GOWORK=off", "GOCACHE=/tmp/tlrpc-generated-go-cache")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("compile and test generated multi-layer package: %v\n%s", err, output)
	}
}

const multiLayerRoundTripTest = `package multilayer_test

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"testing"

	"example.com/multilayer-generation/gen"
	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/mtproto"
)

func TestGeneratedLayerSelectionAndNestedRoundTrip(t *testing.T) {
	if _, ok := reflect.TypeOf(gen.Child{}).FieldByName("Label"); ok {
		t.Fatal("base child retained a layer 229 field instead of using an exact variant")
	}
	oldRequest, ok := gen.NewMethodRequestForLayer(0x20, 228)
	if !ok {
		t.Fatal("missing layer 228 request")
	}
	if _, ok := oldRequest.(*gen.MessagesForwardRequest); !ok {
		t.Fatalf("layer 228 request type = %T", oldRequest)
	}
	newRequest, ok := gen.NewMethodRequestForLayer(0x20, 229)
	if !ok {
		t.Fatal("missing layer 229 request")
	}
	if _, ok := newRequest.(*gen.MessagesForwardRequestLayer229); !ok {
		t.Fatalf("layer 229 request type = %T", newRequest)
	}
	historicalRequest, ok := gen.NewMethodRequestForLayer(0x21, 229)
	if !ok {
		t.Fatal("missing historical request on a newer session")
	}
	if _, ok := historicalRequest.(*gen.MessagesForwardRequestLayer172); !ok {
		t.Fatalf("historical request type = %T", historicalRequest)
	}
	if _, ok := gen.NewMethodRequestForLayer(0x21, 227); ok {
		t.Fatal("generated package accepted a baseline method below its supported floor")
	}
	if _, ok := gen.NewMethodRequestForLayer(0x21, 228); !ok {
		t.Fatal("generated package rejected a historical wire form at its base layer")
	}
	if _, ok := gen.NewMethodRequestForLayer(0x22, 228); ok {
		t.Fatal("layer 228 accepted a method introduced at layer 229")
	}
	newUniqueRequest, ok := gen.NewMethodRequestForLayer(0x22, 229)
	if !ok {
		t.Fatal("layer 229 rejected its newly introduced method")
	}
	if _, ok := newUniqueRequest.(*gen.MessagesNewMethodRequest); !ok {
		t.Fatalf("new layer 229 request type = %T", newUniqueRequest)
	}
	if _, ok := gen.NewMethodRequestForLayer(0x31, 228); ok {
		t.Fatal("layer 228 accepted a changed method's layer-229 ID")
	}
	changedUniqueRequest, ok := gen.NewMethodRequestForLayer(0x31, 229)
	if !ok {
		t.Fatal("layer 229 rejected a changed method's new ID")
	}
	if _, ok := changedUniqueRequest.(*gen.AuthCheckRequestLayer229); !ok {
		t.Fatalf("changed layer 229 request type = %T", changedUniqueRequest)
	}
	if _, ok := gen.NewMethodRequestForLayer(0x30, 228); !ok {
		t.Fatal("layer 228 rejected its original method ID")
	}
	if _, ok := gen.NewMethodRequestForLayer(0x30, 229); ok {
		t.Fatal("layer 229 accepted the replaced layer 228 method ID")
	}
	if _, ok := gen.NewConstructorForLayer(0x02, 229); ok {
		t.Fatal("layer 229 accepted the removed layer 228 constructor ID")
	}
	if _, ok := gen.NewConstructorForLayer(0x05, 228); ok {
		t.Fatal("layer 228 accepted a nested constructor introduced at layer 229")
	}
	if _, ok := gen.NewConstructorForLayer(0x05, 229); !ok {
		t.Fatal("layer 229 rejected its newly introduced nested constructor")
	}

	oldSource := &gen.Container{Child: &gen.Child{Active: true}, Nested: &gen.LegacyNested{Value: 7}}
	var oldWire bytes.Buffer
	if err := oldSource.SerializeTL(mtproto.WithLayerWriter(&oldWire, 228)); err != nil {
		t.Fatal(err)
	}
	var oldDecoded gen.Container
	if err := oldDecoded.DeserializeTL(mtproto.WithLayerReader(bytes.NewReader(oldWire.Bytes()), 228)); err != nil {
		t.Fatal(err)
	}
	if _, ok := oldDecoded.Child.(*gen.Child); !ok {
		t.Fatalf("layer 228 child type = %T", oldDecoded.Child)
	}
	var defaultWire bytes.Buffer
	if err := oldSource.SerializeTL(&defaultWire); err != nil {
		t.Fatal(err)
	}
	var defaultDecoded gen.Container
	if err := defaultDecoded.DeserializeTL(bytes.NewReader(defaultWire.Bytes())); err != nil {
		t.Fatal(err)
	}
	if _, ok := defaultDecoded.Child.(*gen.Child); !ok {
		t.Fatalf("zero layer child type = %T", defaultDecoded.Child)
	}
	if _, ok := oldDecoded.Nested.(*gen.LegacyNested); !ok {
		t.Fatalf("historical nested type = %T", oldDecoded.Nested)
	}

	label := "new"
	newSource := &gen.Container{Child: &gen.ChildLayer229{Active: true, Label: &label}, Nested: &gen.CurrentNested{Text: "current"}}
	var newWire bytes.Buffer
	if err := newSource.SerializeTL(mtproto.WithLayerWriter(&newWire, 229)); err != nil {
		t.Fatal(err)
	}
	var newDecoded gen.Container
	if err := newDecoded.DeserializeTL(mtproto.WithLayerReader(bytes.NewReader(newWire.Bytes()), 229)); err != nil {
		t.Fatal(err)
	}
	child229, ok := newDecoded.Child.(*gen.ChildLayer229)
	if !ok || child229.Label == nil || *child229.Label != "new" {
		t.Fatalf("layer 229 child = %#v", newDecoded.Child)
	}
	if _, ok := newDecoded.Nested.(*gen.CurrentNested); !ok {
		t.Fatalf("layer 229 nested type = %T", newDecoded.Nested)
	}

	wantOld, _ := hex.DecodeString("0300000001000000010000000200000007000000")
	if !bytes.Equal(oldWire.Bytes(), wantOld) {
		t.Fatalf("layer 228 exact bytes = %x, want %x", oldWire.Bytes(), wantOld)
	}

	matrix := &gen.Matrix{Values: [][]int32{{1, 2}, {3}}}
	wantMatrix, _ := hex.DecodeString("0600000015c4b51c0200000015c4b51c02000000010000000200000015c4b51c0100000003000000")
	var matrixWire bytes.Buffer
	if err := matrix.SerializeTL(&matrixWire); err != nil { t.Fatal(err) }
	if !bytes.Equal(matrixWire.Bytes(), wantMatrix) { t.Fatalf("matrix bytes = %x, want %x", matrixWire.Bytes(), wantMatrix) }
	var decodedMatrix gen.Matrix
	if err := decodedMatrix.DeserializeTL(bytes.NewReader(wantMatrix)); err != nil { t.Fatal(err) }
	if !reflect.DeepEqual(matrix, &decodedMatrix) { t.Fatalf("matrix roundtrip = %#v", decodedMatrix) }

	wantRequest, _ := hex.DecodeString("4000000015c4b51c0200000015c4b51c02000000010000000200000015c4b51c0100000003000000")
	requestMatrix := &gen.TestMatrixRequest{Values: [][]int32{{1, 2}, {3}}}
	var requestWire bytes.Buffer
	if err := requestMatrix.SerializeTL(&requestWire); err != nil { t.Fatal(err) }
	if !bytes.Equal(requestWire.Bytes(), wantRequest) { t.Fatalf("nested request bytes = %x, want %x", requestWire.Bytes(), wantRequest) }
	var decodedRequest gen.TestMatrixRequest
	if err := decodedRequest.DeserializeTL(bytes.NewReader(wantRequest)); err != nil { t.Fatal(err) }
	if !reflect.DeepEqual(requestMatrix, &decodedRequest) { t.Fatalf("nested request roundtrip = %#v", decodedRequest) }
	var encodedResponse []byte
	for _, method := range gen.Test_ServiceDesc.Methods {
		if method.ConstructorID == 0x40 {
			var err error
			encodedResponse, err = method.EncodeResponse(matrix, 228, tlrpc.EncodeLimits{})
			if err != nil { t.Fatal(err) }
		}
	}
	if !bytes.Equal(encodedResponse, wantMatrix) { t.Fatalf("nested response bytes = %x, want %x", encodedResponse, wantMatrix) }

	stale := "stale"
	reused := gen.ChildLayer229{Label: &stale}
	var clearWire bytes.Buffer
	_ = mtproto.WriteUint32(&clearWire, 0x00000001)
	_ = mtproto.WriteUint32(&clearWire, 1)
	if err := reused.DeserializeTL(mtproto.WithLayerReader(bytes.NewReader(clearWire.Bytes()), 229)); err != nil { t.Fatal(err) }
	if reused.Label != nil || !reused.Active { t.Fatalf("reused receiver retained optional state: %#v", reused) }
	reused.Label = &stale
	truncated := clearWire.Bytes()[:4]
	if err := reused.DeserializeTL(mtproto.WithLayerReader(bytes.NewReader(truncated), 229)); err == nil { t.Fatal("truncated decode succeeded") }
	if reused.Label != nil || reused.Active { t.Fatalf("malformed decode retained receiver state: %#v", reused) }

	var malformed bytes.Buffer
	_ = mtproto.WriteUint32(&malformed, 0x00000001)
	_ = mtproto.WriteUint32(&malformed, 1<<1)
	var child gen.Child
	if err := child.DeserializeTL(mtproto.WithLayerReader(bytes.NewReader(malformed.Bytes()), 228)); err == nil {
		t.Fatal("layer 228 accepted a layer 229-only flag")
	}
	var malformedRequest bytes.Buffer
	_ = mtproto.WriteUint32(&malformedRequest, 0x00000020)
	_ = mtproto.WriteUint32(&malformedRequest, 1<<1)
	var request gen.MessagesForwardRequest
	if err := request.DeserializeTL(mtproto.WithLayerReader(bytes.NewReader(malformedRequest.Bytes()), 228)); err == nil {
		t.Fatal("layer 228 request accepted a layer 229-only flag")
	}
}
`
