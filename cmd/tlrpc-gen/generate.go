package main

import (
	"fmt"

	"github.com/r6m/tlrpc/pkg/codegen"
)

// generateCommand handles the code generation logic
type generateCommand struct {
	schemaPath string
	outDir     string
	pkgName    string
	layers     []int
}

func newGenerateCommand(schemaPath, outDir, pkgName string, layers []int) *generateCommand {
	return &generateCommand{
		schemaPath: schemaPath,
		outDir:     outDir,
		pkgName:    pkgName,
		layers:     layers,
	}
}

func (g *generateCommand) run() error {
	// Validate inputs
	if g.schemaPath == "" {
		return fmt.Errorf("schema path is required")
	}

	// Parse schema
	parser := codegen.NewParser()
	schema, err := parser.ParseFile(g.schemaPath)
	if err != nil {
		return fmt.Errorf("parse schema: %w", err)
	}

	// Create generator
	generator := codegen.NewGenerator(codegen.Options{
		Package: g.pkgName,
		Layers:  g.layers,
		OutDir:  g.outDir,
	})

	// Generate code
	if err := generator.Generate(schema); err != nil {
		return fmt.Errorf("generate code: %w", err)
	}

	fmt.Printf("Successfully generated code in %s\n", g.outDir)
	return nil
}
