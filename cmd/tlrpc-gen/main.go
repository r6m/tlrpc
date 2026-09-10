package main

import (
	"crypto/sha256"
	"encoding/binary"
	"flag"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/r6m/tlrpc/internal/generator"
	"github.com/r6m/tlrpc/internal/naming"
	"github.com/r6m/tlrpc/internal/parser"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tlrpc-gen", flag.ContinueOnError)
	fs.SetOutput(stderr)

	schemaPath := fs.String("schema", "", "Path to TL schema file (required)")
	outDir := fs.String("out", "./gen", "Output directory")
	pkgName := fs.String("package", "gen", "Go package name")
	layerFlag := fs.String("layer", "", "Layer to generate")
	var layers layerListFlags
	fs.Var(&layers, "layers", "Layers to generate into one package (comma-separated or repeatable)")
	baseLayer := fs.Int("base-layer", 0, "Layer represented by the base schema when using --layer-diff")
	var layerDiffs layerDiffFlags
	fs.Var(&layerDiffs, "layer-diff", "Ordered layer difference as <layer>:<path> (repeatable)")
	verbose := fs.Bool("verbose", false, "Verbose logging")

	if err := fs.Parse(args); err != nil {
		return 3
	}
	if *schemaPath == "" {
		_, _ = fmt.Fprintln(stderr, "--schema is required")
		fs.Usage()
		return 3
	}

	layer, err := parseLayer(*layerFlag)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 3
	}
	if len(layerDiffs) > 0 {
		if *baseLayer <= 0 {
			_, _ = fmt.Fprintln(stderr, "--base-layer must be a positive integer when --layer-diff is used")
			return 3
		}
		if layer <= 0 && len(layers) == 0 {
			_, _ = fmt.Fprintln(stderr, "--layer must be a positive integer when --layer-diff is used")
			return 3
		}
	}
	if layer > 0 && len(layers) > 0 {
		_, _ = fmt.Fprintln(stderr, "--layer and --layers are mutually exclusive")
		return 3
	}
	if len(layers) > 0 {
		if *baseLayer <= 0 {
			_, _ = fmt.Fprintln(stderr, "--base-layer must be a positive integer when --layers is used")
			return 3
		}
		if err := validateLayerList(layers, *baseLayer, layerDiffs); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 3
		}
	}
	data, err := os.ReadFile(*schemaPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "read schema: %v\n", err)
		return 3
	}

	var schema *parser.Schema
	source := filepath.Base(*schemaPath)
	schemaDigest := fmt.Sprintf("%x", sha256.Sum256(data))
	if len(layers) > 0 {
		schema, source, schemaDigest, err = resolveSchemaLayerSet(*schemaPath, data, *baseLayer, layerDiffs)
	} else if len(layerDiffs) > 0 {
		schema, source, schemaDigest, err = resolveSchemaLayers(*schemaPath, data, *baseLayer, layer, layerDiffs)
	} else {
		schema, err = parser.ParseBaselineSchema(string(data), layer, source)
		if err == nil && schema.Layer > 0 {
			schema, err = parser.ResolveSelectedLayer(schema, schema.Layer, schema.Layer, nil)
		}
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	validator := parser.NewValidator(schema)
	if err := validator.Validate(); err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	if *verbose {
		_, _ = fmt.Fprintf(stdout, "Parsing schema: %s...\n", *schemaPath)
		_, _ = fmt.Fprintf(stdout, "Found %d constructors, %d functions\n", len(schema.Constructors), len(schema.Functions))
	}

	writer := generator.NewFileWriterWithProvenance(
		*outDir,
		*pkgName,
		source,
		schema.Layer,
		schemaDigest,
	)
	namer := naming.NewNamer()

	typesOut := writer.NewFile("types.go")
	var interfacesOut io.Writer
	if hasUnionTypes(schema) {
		interfacesOut = writer.NewFile("interfaces.go")
	}
	servicesOut := writer.NewFile("services.go")
	registerOut := writer.NewFile("register.go")
	requestsOut := writer.NewFile("requests.go")
	constantsOut := writer.NewFile("constants.go")
	codecOut := writer.NewFile("codec.go")
	baseAliasesOut := writer.NewFile("base_aliases.go")

	if *verbose {
		_, _ = fmt.Fprintln(stdout, "Generating types...")
	}
	typeGen := generator.NewTypeGenerator(namer, typesOut, schema)
	var ifaceGen *generator.TypeGenerator
	if interfacesOut != nil {
		ifaceGen = generator.NewTypeGenerator(namer, interfacesOut, schema)
	}
	for i := range schema.Types {
		if err := typeGen.GenerateType(&schema.Types[i]); err != nil {
			_, _ = fmt.Fprintf(stderr, "generate types: %v\n", err)
			return 2
		}
		if interfacesOut != nil {
			if err := ifaceGen.GenerateInterface(&schema.Types[i]); err != nil {
				_, _ = fmt.Fprintf(stderr, "generate interfaces: %v\n", err)
				return 2
			}
		}
	}

	if *verbose {
		_, _ = fmt.Fprintln(stdout, "Generating services...")
	}
	serviceGen := generator.NewServiceGenerator(namer, schema, servicesOut)
	if err := serviceGen.GenerateService(schema.Functions); err != nil {
		_, _ = fmt.Fprintf(stderr, "generate services: %v\n", err)
		return 2
	}
	regGen := generator.NewServiceGenerator(namer, schema, registerOut)
	if err := regGen.GenerateRegistration(schema.Functions); err != nil {
		_, _ = fmt.Fprintf(stderr, "generate registration: %v\n", err)
		return 2
	}
	reqGen := generator.NewServiceGenerator(namer, schema, requestsOut)
	if err := reqGen.GenerateRequests(schema.Functions); err != nil {
		_, _ = fmt.Fprintf(stderr, "generate requests: %v\n", err)
		return 2
	}

	if *verbose {
		_, _ = fmt.Fprintln(stdout, "Generating codec registry...")
	}
	codecGen := generator.NewCodecGenerator(namer, codecOut)
	if err := codecGen.Generate(schema); err != nil {
		_, _ = fmt.Fprintf(stderr, "generate codec: %v\n", err)
		return 2
	}
	if schema.IsLayered {
		projectionGen := generator.NewProjectionGenerator(namer, writer.NewFile("projection.go"))
		if err := projectionGen.Generate(schema); err != nil {
			_, _ = fmt.Fprintf(stderr, "generate projection: %v\n", err)
			return 2
		}
	}

	if *verbose {
		_, _ = fmt.Fprintln(stdout, "Generating constants...")
	}
	if err := generator.GenerateSchemaMetadata(constantsOut, schema.Layer); err != nil {
		_, _ = fmt.Fprintf(stderr, "generate schema metadata: %v\n", err)
		return 2
	}
	if schema.IsLayered {
		if err := generator.GenerateLayeredSchemaMetadata(constantsOut, schema.BaseLayer, layers); err != nil {
			_, _ = fmt.Fprintf(stderr, "generate layered schema metadata: %v\n", err)
			return 2
		}
	}
	if err := generator.GenerateConstructorConstants(namer, constantsOut, schema.Constructors); err != nil {
		_, _ = fmt.Fprintf(stderr, "generate constants: %v\n", err)
		return 2
	}
	if err := generator.GenerateBaseAliases(baseAliasesOut); err != nil {
		_, _ = fmt.Fprintf(stderr, "generate base aliases: %v\n", err)
		return 2
	}

	if *verbose {
		_, _ = fmt.Fprintf(stdout, "Writing files to %s...\n", *outDir)
	}
	if err := writer.WriteAll(); err != nil {
		_, _ = fmt.Fprintf(stderr, "write files: %v\n", err)
		return 3
	}
	if *verbose {
		_, _ = fmt.Fprintln(stdout, "Formatting with gofmt...")
	}
	if err := writer.Format(); err != nil {
		_, _ = fmt.Fprintf(stderr, "format: %v\n", err)
		return 3
	}

	if *verbose {
		_, _ = fmt.Fprintf(stdout, "Done. Generated %d files in %s\n", len(writer.Files()), *outDir)
	}

	return 0
}

type layerDiffSpec struct {
	Layer int
	Path  string
}

type layerDiffFlags []layerDiffSpec

type layerListFlags []int

func (f *layerListFlags) String() string {
	values := make([]string, len(*f))
	for i, layer := range *f {
		values[i] = strconv.Itoa(layer)
	}
	return strings.Join(values, ",")
}

func (f *layerListFlags) Set(raw string) error {
	for _, value := range strings.Split(raw, ",") {
		layer, err := parseLayer(value)
		if err != nil {
			return fmt.Errorf("invalid --layers %q: %w", raw, err)
		}
		*f = append(*f, layer)
	}
	return nil
}

func validateLayerList(layers []int, baseLayer int, differences []layerDiffSpec) error {
	want := make([]int, 1, len(differences)+1)
	want[0] = baseLayer
	for _, difference := range differences {
		want = append(want, difference.Layer)
	}
	if len(layers) != len(want) {
		return fmt.Errorf("--layers must list base layer %d followed by every --layer-diff layer", baseLayer)
	}
	for i := range want {
		if layers[i] != want[i] {
			return fmt.Errorf("--layers must be ordered as %v", want)
		}
	}
	return nil
}

func (f *layerDiffFlags) String() string {
	values := make([]string, len(*f))
	for i, diff := range *f {
		values[i] = fmt.Sprintf("%d:%s", diff.Layer, diff.Path)
	}
	return strings.Join(values, ",")
}

func (f *layerDiffFlags) Set(raw string) error {
	diff, err := parseLayerDiff(raw)
	if err != nil {
		return err
	}
	*f = append(*f, diff)
	return nil
}

func parseLayerDiff(raw string) (layerDiffSpec, error) {
	layerText, path, ok := strings.Cut(raw, ":")
	if !ok || strings.TrimSpace(path) == "" {
		return layerDiffSpec{}, fmt.Errorf("invalid --layer-diff %q: expected <layer>:<path>", raw)
	}
	layer, err := parseLayer(layerText)
	if err != nil || layer <= 0 {
		return layerDiffSpec{}, fmt.Errorf("invalid --layer-diff %q: expected a positive layer before ':'", raw)
	}
	return layerDiffSpec{Layer: layer, Path: path}, nil
}

type layerDiffInput struct {
	spec layerDiffSpec
	data []byte
}

func resolveSchemaLayers(basePath string, baseData []byte, baseLayer, targetLayer int, specs []layerDiffSpec) (*parser.Schema, string, string, error) {
	base, err := parser.ParseBaselineSchema(string(baseData), baseLayer, basePath)
	if err != nil {
		return nil, "", "", err
	}

	inputs := make([]layerDiffInput, 0, len(specs))
	differences := make([]parser.LayerDifference, 0, len(specs))
	for _, spec := range specs {
		data, err := os.ReadFile(spec.Path)
		if err != nil {
			return nil, "", "", fmt.Errorf("read layer difference %d from %s: %w", spec.Layer, spec.Path, err)
		}
		difference, err := parser.ParseLayerDifference(string(data), spec.Layer, spec.Path)
		if err != nil {
			return nil, "", "", err
		}
		inputs = append(inputs, layerDiffInput{spec: spec, data: data})
		differences = append(differences, difference)
	}

	resolved, err := parser.ResolveSelectedLayer(base, baseLayer, targetLayer, differences)
	if err != nil {
		return nil, "", "", err
	}

	selected := selectedLayerDiffInputs(inputs, baseLayer, targetLayer)
	sources := make([]string, 1, len(selected)+1)
	sources[0] = filepath.Base(basePath)
	digest := sha256.New()
	writeDigestFrame(digest, baseData)
	for _, input := range selected {
		sources = append(sources, filepath.Base(input.spec.Path))
		writeDigestFrame(digest, input.data)
	}
	return resolved, strings.Join(sources, " + "), fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func resolveSchemaLayerSet(basePath string, baseData []byte, baseLayer int, specs []layerDiffSpec) (*parser.Schema, string, string, error) {
	base, err := parser.ParseBaselineSchema(string(baseData), baseLayer, basePath)
	if err != nil {
		return nil, "", "", err
	}
	inputs := make([]layerDiffInput, 0, len(specs))
	differences := make([]parser.LayerDifference, 0, len(specs))
	for _, spec := range specs {
		data, err := os.ReadFile(spec.Path)
		if err != nil {
			return nil, "", "", fmt.Errorf("read layer difference %d from %s: %w", spec.Layer, spec.Path, err)
		}
		difference, err := parser.ParseLayerDifference(string(data), spec.Layer, spec.Path)
		if err != nil {
			return nil, "", "", err
		}
		inputs = append(inputs, layerDiffInput{spec: spec, data: data})
		differences = append(differences, difference)
	}
	layered, err := parser.ResolveLayers(base, baseLayer, differences)
	if err != nil {
		return nil, "", "", err
	}
	sources := []string{filepath.Base(basePath)}
	digest := sha256.New()
	writeDigestFrame(digest, baseData)
	for _, input := range inputs {
		sources = append(sources, filepath.Base(input.spec.Path))
		writeDigestFrame(digest, input.data)
	}
	return layered.Schema, strings.Join(sources, " + "), fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func selectedLayerDiffInputs(inputs []layerDiffInput, baseLayer, targetLayer int) []layerDiffInput {
	selected := make([]layerDiffInput, 0, len(inputs))
	for _, input := range inputs {
		if input.spec.Layer > baseLayer && input.spec.Layer <= targetLayer {
			selected = append(selected, input)
		}
	}
	return selected
}

func writeDigestFrame(digest hash.Hash, data []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(data)))
	_, _ = digest.Write(size[:])
	_, _ = digest.Write(data)
}

func parseLayer(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	if strings.Contains(raw, ",") {
		return 0, fmt.Errorf("one generated package represents exactly one layer; pass a single value to --layer")
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid layer %q: expected a positive integer", raw)
	}
	return value, nil
}

func hasUnionTypes(schema *parser.Schema) bool {
	for i := range schema.Types {
		if naming.IsBuiltinType(schema.Types[i].Name) {
			continue
		}
		if schema.IsLayered || schema.Types[i].IsUnion || len(schema.Types[i].Constructors) > 1 {
			return true
		}
	}
	return false
}
