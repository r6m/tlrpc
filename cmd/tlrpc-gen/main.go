package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/r6m/tlrpc/pkg/codegen"
)

func main() {
	var (
		schemaPath = flag.String("schema", "", "Path to TL schema file (required)")
		outDir     = flag.String("out", "./gen", "Output directory")
		pkgName    = flag.String("package", "gen", "Go package name")
		layersStr  = flag.String("layers", "", "Comma-separated layer versions (auto-detect if empty)")
	)
	flag.Parse()

	if *schemaPath == "" {
		fmt.Fprintf(os.Stderr, "Error: --schema is required\n")
		flag.Usage()
		os.Exit(1)
	}

	// Parse layers
	var layers []int
	if *layersStr != "" {
		for _, s := range strings.Split(*layersStr, ",") {
			var layer int
			if _, err := fmt.Sscanf(s, "%d", &layer); err != nil {
				fmt.Fprintf(os.Stderr, "Error: invalid layer %q\n", s)
				os.Exit(1)
			}
			layers = append(layers, layer)
		}
	}

	// Ensure output directory exists
	if err := os.MkdirAll(*outDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: create output dir: %v\n", err)
		os.Exit(1)
	}

	// Parse schema
	parser := codegen.NewParser()
	schema, err := parser.ParseFile(*schemaPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: parse schema: %v\n", err)
		os.Exit(1)
	}

	// Auto-detect layer if not specified
	if len(layers) == 0 {
		layers = []int{detectLayer(schema)}
	}

	// Generate code
	generator := codegen.NewGenerator(codegen.Options{
		Package: *pkgName,
		Layers:  layers,
		OutDir:  *outDir,
	})

	if err := generator.Generate(schema); err != nil {
		fmt.Fprintf(os.Stderr, "Error: generate code: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated code for layer %v in %s\n", layers, *outDir)
}

func detectLayer(schema *codegen.Schema) int {
	// Try to detect from schema comments or filename
	// Default to 222
	return 222
}
