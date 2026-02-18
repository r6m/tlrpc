package main

import (
	"flag"
	"fmt"
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
	layers := fs.String("layers", "", "Comma-separated layer versions")
	verbose := fs.Bool("verbose", false, "Verbose logging")

	if err := fs.Parse(args); err != nil {
		return 3
	}
	if *schemaPath == "" {
		_, _ = fmt.Fprintln(stderr, "--schema is required")
		fs.Usage()
		return 3
	}

	data, err := os.ReadFile(*schemaPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "read schema: %v\n", err)
		return 3
	}

	p := parser.NewParser(string(data))
	layer := parseLayer(*layers)
	var schema *parser.Schema
	if layer > 0 {
		schema, err = p.ParseWithLayer(layer)
	} else {
		schema, err = p.Parse()
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

	writer := generator.NewFileWriter(*outDir, *pkgName, filepath.Base(*schemaPath), schema.Layer)
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
	for i := range schema.Types {
		gen := generator.NewTypeGenerator(namer, typesOut, schema)
		if err := gen.GenerateType(&schema.Types[i]); err != nil {
			_, _ = fmt.Fprintf(stderr, "generate types: %v\n", err)
			return 2
		}
		if interfacesOut != nil {
			ifaceGen := generator.NewTypeGenerator(namer, interfacesOut, schema)
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

	if *verbose {
		_, _ = fmt.Fprintln(stdout, "Generating constants...")
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

func parseLayer(raw string) int {
	if raw == "" {
		return 0
	}
	parts := strings.Split(raw, ",")
	if len(parts) == 0 {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0
	}
	return value
}

func hasUnionTypes(schema *parser.Schema) bool {
	for i := range schema.Types {
		if schema.Types[i].IsUnion || len(schema.Types[i].Constructors) > 1 {
			return true
		}
	}
	return false
}
