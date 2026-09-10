package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const boxedChannelApplication = `package application

import (
	"bytes"
	"fmt"
	"testing"

	"example.com/boxed-channel/gen"
)

var _ = gen.Channel{AdminRights: &gen.ChatAdminRights{ChangeInfo: true}}

func TestChannelRejectsNilRequiredAdminRights(t *testing.T) {
	for _, channel := range []*gen.Channel{
		{},
		func() *gen.Channel {
			var rights *gen.ChatAdminRights
			return &gen.Channel{AdminRights: rights}
		}(),
	} {
		if err := serializeChannelWithoutPanic(channel); err == nil {
			t.Fatalf("SerializeTL(%#v) succeeded with nil required admin rights", channel)
		}
	}
}

func serializeChannelWithoutPanic(channel *gen.Channel) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("SerializeTL panicked: %v", recovered)
		}
	}()
	return channel.SerializeTL(&bytes.Buffer{})
}
`

func TestRun_LayeredBoxedChildKeepsStableParentApplication(t *testing.T) {
	basePath := writeTestFile(t, "base-228.tl", `---types---
chatAdminRights#00000001 flags:# change_info:flags.0?true = ChatAdminRights;
channel#00000002 admin_rights:ChatAdminRights = Channel;
---functions---
test.echo#00000003 channel:Channel = Channel;
`)
	deltaPath := writeTestFile(t, "delta-229.tl", `---types---
chatAdminRights#00000001 flags:# change_info:flags.0?true other:flags.1?true = ChatAdminRights;
`)

	for _, layers := range [][]int{{228}, {228, 229}} {
		t.Run(fmt.Sprint(layers), func(t *testing.T) {
			moduleDir := t.TempDir()
			outDir := filepath.Join(moduleDir, "gen")
			layerArgs := []string{"228"}
			if len(layers) == 2 {
				layerArgs = append(layerArgs, "229")
			}
			args := []string{
				"--schema=" + basePath,
				"--base-layer=228",
				"--layers=" + strings.Join(layerArgs, ","),
				"--out=" + outDir,
				"--package=gen",
			}
			if len(layers) == 2 {
				args = append(args, "--layer-diff=229:"+deltaPath)
			}
			var stderr strings.Builder
			if code := run(args, io.Discard, &stderr); code != 0 {
				t.Fatalf("run returned %d: %s", code, stderr.String())
			}
			requireLayeredGeneratedFiles(t, outDir)
			if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(layeredCLIGoMod("example.com/boxed-channel")), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(moduleDir, "application_test.go"), []byte(boxedChannelApplication), 0o600); err != nil {
				t.Fatal(err)
			}
			compileLayeredCLIApplication(t, moduleDir)

			if len(layers) == 2 {
				types, err := os.ReadFile(filepath.Join(outDir, "types.go"))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(types), "type ChannelLayer229 struct") {
					t.Fatal("changed boxed child generated a parent layer variant")
				}
			}
		})
	}
}

func TestRun_LayeredSingletonFamilyUsesConstructorDerivedNames(t *testing.T) {
	basePath := writeTestFile(t, "base-228.tl", `---types---
only#00000001 value:int = Family;
parent#00000002 family:Family = Parent;
---functions---
test.echo#00000004 parent:Parent = Parent;
`)
	deltaPath := writeTestFile(t, "delta-229.tl", `---types---
other#00000003 text:string = Family;
`)

	const application = `package application

import "example.com/singleton-family/gen"

var _ = &gen.Parent{Family: &gen.Only{Value: 1}}
`
	for _, layers := range [][]int{{228}, {228, 229}} {
		t.Run(fmt.Sprint(layers), func(t *testing.T) {
			moduleDir := t.TempDir()
			outDir := filepath.Join(moduleDir, "gen")
			layerArgs := []string{"228"}
			if len(layers) == 2 {
				layerArgs = append(layerArgs, "229")
			}
			args := []string{
				"--schema=" + basePath,
				"--base-layer=228",
				"--layers=" + strings.Join(layerArgs, ","),
				"--out=" + outDir,
				"--package=gen",
			}
			if len(layers) == 2 {
				args = append(args, "--layer-diff=229:"+deltaPath)
			}
			var stderr strings.Builder
			if code := run(args, io.Discard, &stderr); code != 0 {
				t.Fatalf("run returned %d: %s", code, stderr.String())
			}
			requireLayeredGeneratedFiles(t, outDir)
			assertConstructorDerivedFamilyName(t, outDir, len(layers) == 2)
			if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(layeredCLIGoMod("example.com/singleton-family")), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(moduleDir, "application_test.go"), []byte(application), 0o600); err != nil {
				t.Fatal(err)
			}
			compileLayeredCLIApplication(t, moduleDir)
		})
	}
}

func TestRun_LayeredSingletonBoxedCodecRoundTrip(t *testing.T) {
	basePath := writeTestFile(t, "base-228.tl", `---types---
only#00000001 value:int = Family;
parent#00000002 flags:# required:Family optional:flags.0?Family items:Vector<Family> = Parent;
`)
	moduleDir := t.TempDir()
	outDir := filepath.Join(moduleDir, "gen")
	var stderr strings.Builder
	if code := run([]string{
		"--schema=" + basePath,
		"--base-layer=228",
		"--layers=228",
		"--out=" + outDir,
		"--package=gen",
	}, io.Discard, &stderr); code != 0 {
		t.Fatalf("run returned %d: %s", code, stderr.String())
	}
	requireLayeredGeneratedFiles(t, outDir)
	types, err := os.ReadFile(filepath.Join(outDir, "types.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"Required *Only", "Optional *Only", "Items    []*Only"} {
		if !strings.Contains(string(types), field) {
			t.Fatalf("missing concrete singleton field %q:\n%s", field, types)
		}
	}
	interfaces, err := os.ReadFile(filepath.Join(outDir, "interfaces.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(interfaces), "type FamilyType interface") || strings.Contains(string(interfaces), "isFamilyType") {
		t.Fatalf("singleton family generated a speculative interface:\n%s", interfaces)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(layeredCLIGoMod("example.com/singleton-codec")), 0o600); err != nil {
		t.Fatal(err)
	}
	const application = `package application

import (
	"bytes"
	"testing"

	"example.com/singleton-codec/gen"
)

func TestConcreteBoxedSingletonCodec(t *testing.T) {
	source := &gen.Parent{
		Required: &gen.Only{Value: 1},
		Optional: &gen.Only{Value: 2},
		Items: []*gen.Only{{Value: 3}, {Value: 4}},
	}
	var encoded bytes.Buffer
	if err := source.SerializeTL(&encoded); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	var decoded gen.Parent
	if err := decoded.DeserializeTL(bytes.NewReader(encoded.Bytes())); err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	if decoded.Required == nil || decoded.Required.Value != 1 || decoded.Optional == nil || decoded.Optional.Value != 2 {
		t.Fatalf("decoded singleton fields = %#v", decoded)
	}
	if len(decoded.Items) != 2 || decoded.Items[0] == nil || decoded.Items[0].Value != 3 || decoded.Items[1] == nil || decoded.Items[1].Value != 4 {
		t.Fatalf("decoded singleton vector = %#v", decoded.Items)
	}

	withoutOptional := &gen.Parent{Required: &gen.Only{Value: 5}, Items: []*gen.Only{}}
	encoded.Reset()
	if err := withoutOptional.SerializeTL(&encoded); err != nil {
		t.Fatalf("serialize without optional: %v", err)
	}
	decoded.Optional = &gen.Only{Value: 99}
	if err := decoded.DeserializeTL(bytes.NewReader(encoded.Bytes())); err != nil {
		t.Fatalf("deserialize without optional: %v", err)
	}
	if decoded.Optional != nil {
		t.Fatalf("missing optional retained stale value: %#v", decoded.Optional)
	}
	if decoded.Items == nil || len(decoded.Items) != 0 {
		t.Fatalf("empty singleton vector = %#v, want non-nil empty", decoded.Items)
	}

	if err := (&gen.Parent{Items: []*gen.Only{}}).SerializeTL(&bytes.Buffer{}); err == nil {
		t.Fatal("nil required boxed singleton serialized successfully")
	}
}
`
	if err := os.WriteFile(filepath.Join(moduleDir, "application_test.go"), []byte(application), 0o600); err != nil {
		t.Fatal(err)
	}
	compileLayeredCLIApplication(t, moduleDir)
}

func TestRun_LayeredResultFamilyGeneratesTwoHandlersForOneRequest(t *testing.T) {
	basePath := writeTestFile(t, "base-228.tl", `---types---
oldResult#00000001 value:int = OldResult;
---functions---
messages.get#00000020 id:int = OldResult;
`)
	deltaPath := writeTestFile(t, "delta-229.tl", `---types---
newResult#00000002 text:string = NewResult;
---functions---
messages.get#00000020 id:int = NewResult;
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
	requireLayeredGeneratedFiles(t, outDir)
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(layeredCLIGoMod("example.com/result-family")), 0o600); err != nil {
		t.Fatal(err)
	}
	const application = `package application

import (
	"context"

	"example.com/result-family/gen"
	"github.com/r6m/tlrpc"
)

type messagesService struct {
	gen.UnimplementedMessagesServer
}

func (messagesService) Get(context.Context, *gen.MessagesGetRequest) (*gen.OldResult, error) {
	return &gen.OldResult{}, nil
}

func (messagesService) GetLayer229(context.Context, *gen.MessagesGetRequest) (*gen.NewResult, error) {
	return &gen.NewResult{}, nil
}

var _ gen.MessagesServer = messagesService{}

func register(server *tlrpc.Server) {
	gen.RegisterMessagesServer(server, messagesService{})
}
`
	if err := os.WriteFile(filepath.Join(moduleDir, "application_test.go"), []byte(application), 0o600); err != nil {
		t.Fatal(err)
	}
	compileLayeredCLIApplication(t, moduleDir)
}

func TestRun_HistoricalVariantRequiresAcceptLayersForEveryBaselineSelection(t *testing.T) {
	basePath := writeTestFile(t, "base-228.tl", `---types---
result#00000001 = Result;
---functions---
// @tlrpc variant-layer 172
messages.get#00000020 = Result;
messages.get#00000021 = Result;
`)
	deltaPath := writeTestFile(t, "delta-229.tl", "---types---\n")

	for _, args := range [][]string{
		{
			"--schema=" + basePath,
			"--layer=228",
			"--out=" + t.TempDir(),
		},
		{
			"--schema=" + basePath,
			"--base-layer=228",
			"--layer=229",
			"--layer-diff=229:" + deltaPath,
			"--out=" + t.TempDir(),
		},
	} {
		var stderr strings.Builder
		if code := run(args, io.Discard, &stderr); code != 1 {
			t.Fatalf("run returned %d, want 1; stderr: %s", code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "requires accept-layers") {
			t.Fatalf("stderr = %q, want missing accept-layers error", stderr.String())
		}
	}
}

func TestRun_SingleTargetOmitsHistoricalVariantOutsideAcceptedRange(t *testing.T) {
	basePath := writeTestFile(t, "base-228.tl", `---types---
result#00000001 = Result;
---functions---
// @tlrpc variant-layer 172
// @tlrpc accept-layers 228-228
messages.get#00000020 = Result;
messages.get#00000021 = Result;
`)
	deltaPath := writeTestFile(t, "delta-229.tl", "---types---\n")
	moduleDir := t.TempDir()
	outDir := filepath.Join(moduleDir, "gen")
	var stderr strings.Builder
	if code := run([]string{
		"--schema=" + basePath,
		"--base-layer=228",
		"--layer=229",
		"--layer-diff=229:" + deltaPath,
		"--out=" + outDir,
		"--package=gen",
	}, io.Discard, &stderr); code != 0 {
		t.Fatalf("run returned %d: %s", code, stderr.String())
	}
	requests, err := os.ReadFile(filepath.Join(outDir, "requests.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(requests), "MessagesGetRequestLayer172") {
		t.Fatal("single-target layer 229 output retained a historical request outside its accepted range")
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(layeredCLIGoMod("example.com/single-target-history")), 0o600); err != nil {
		t.Fatal(err)
	}
	compileLayeredCLIApplication(t, moduleDir)
}

func TestRun_LayeredGenerationIsDeterministic(t *testing.T) {
	basePath := writeTestFile(t, "base-228.tl", `---types---
child#00000001 enabled:Bool = Child;
container#00000002 child:Child = Container;
`)
	deltaPath := writeTestFile(t, "delta-229.tl", `---types---
child#00000001 flags:# enabled:Bool label:flags.0?string = Child;
`)

	generate := func(outDir string) {
		t.Helper()
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
	}

	first := t.TempDir()
	second := t.TempDir()
	generate(first)
	generate(second)
	if got, want := generatedFileContents(t, second), generatedFileContents(t, first); !reflect.DeepEqual(got, want) {
		t.Fatal("repeated layered generation produced different files")
	}
}

func TestRun_LayeredPrimitiveOnlySchemaWritesEmptyInterfacesCategory(t *testing.T) {
	basePath := writeTestFile(t, "base-228.tl", `---types---
---functions---
ping#00000001 value:int = Bool;
`)
	moduleDir := t.TempDir()
	outDir := filepath.Join(moduleDir, "gen")
	var stderr strings.Builder
	if code := run([]string{
		"--schema=" + basePath,
		"--base-layer=228",
		"--layers=228",
		"--out=" + outDir,
		"--package=gen",
	}, io.Discard, &stderr); code != 0 {
		t.Fatalf("run returned %d: %s", code, stderr.String())
	}
	interfaces, err := os.ReadFile(filepath.Join(outDir, "interfaces.go"))
	if err != nil {
		t.Fatalf("read primitive-only interfaces.go: %v", err)
	}
	if strings.Contains(string(interfaces), " interface {") {
		t.Fatalf("primitive-only layered schema generated an interface:\n%s", interfaces)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(layeredCLIGoMod("example.com/primitive-only")), 0o600); err != nil {
		t.Fatal(err)
	}
	compileLayeredCLIApplication(t, moduleDir)
}

func requireLayeredGeneratedFiles(t *testing.T, outDir string) {
	t.Helper()
	for _, name := range []string{
		"base_aliases.go",
		"codec.go",
		"constants.go",
		"interfaces.go",
		"projection.go",
		"register.go",
		"requests.go",
		"services.go",
		"types.go",
	} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Fatalf("missing generated %s: %v", name, err)
		}
	}
	matches, err := filepath.Glob(filepath.Join(outDir, "*_layer229.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("layered output split generated categories by layer: %v", matches)
	}
}

func assertConstructorDerivedFamilyName(t *testing.T, outDir string, wantInterface bool) {
	t.Helper()
	types, err := os.ReadFile(filepath.Join(outDir, "types.go"))
	if err != nil {
		t.Fatal(err)
	}
	generated := string(types)
	if !strings.Contains(generated, "type Only struct") {
		t.Fatal("multi-layer singleton family did not generate its concrete type from the constructor name")
	}
	interfaces, err := os.ReadFile(filepath.Join(outDir, "interfaces.go"))
	if err != nil {
		t.Fatal(err)
	}
	if wantInterface {
		if !strings.Contains(generated, "Family FamilyType") || !strings.Contains(string(interfaces), "type FamilyType interface") {
			t.Fatal("expanded family did not use its generated interface")
		}
	} else {
		if !strings.Contains(generated, "Family *Only") {
			t.Fatal("singleton family parent did not use its concrete pointer")
		}
		if strings.Contains(string(interfaces), "type FamilyType interface") || strings.Contains(string(interfaces), "isFamilyType") {
			t.Fatal("singleton family generated a speculative interface")
		}
	}
	for _, forbidden := range []string{"type Family struct", "type Family = Only"} {
		if strings.Contains(generated, forbidden) {
			t.Fatalf("multi-layer singleton family retained a family-derived concrete name: %q", forbidden)
		}
	}
	if strings.Contains(generated, "ParentLayer229") {
		t.Fatal("family expansion generated an unnecessary parent layer variant")
	}
}

func layeredCLIGoMod(module string) string {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("module %s\n\ngo 1.25\n\nrequire github.com/r6m/tlrpc v0.0.0\n\nreplace github.com/r6m/tlrpc => %s\n", module, root)
}

func compileLayeredCLIApplication(t *testing.T, moduleDir string) {
	t.Helper()
	command := exec.Command("go", "test", "-mod=mod", "-p=1", "./...")
	command.Dir = moduleDir
	command.Env = append(os.Environ(), "GOWORK=off", "GOCACHE=/tmp/tlrpc-generated-go-cache")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("compile generated application: %v\n%s", err, output)
	}
}

func generatedFileContents(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = contents
	}
	return files
}
