// Package codegen provides AST types for TL schema.
package codegen

// Schema represents a complete TL schema.
type Schema struct {
	Types     []*Type
	Functions []*Function
}

// Type represents a TL type definition.
type Type struct {
	Name          string
	ConstructorID uint32
	Params        []*Param
	ResultType    string
}

// Function represents a TL function definition.
type Function struct {
	Name          string
	ConstructorID uint32
	Params        []*Param
	ResultType    string
}

// Param represents a parameter in a type or function.
type Param struct {
	Name string
	Type string
}

// Options represents code generation options.
type Options struct {
	Package string
	Layers  []int
	OutDir  string
}

// Generator generates code from schema.
type Generator struct {
	options Options
}

// NewGenerator creates a new code generator.
func NewGenerator(options Options) *Generator {
	return &Generator{options: options}
}

// Generate generates code from schema.
func (g *Generator) Generate(schema *Schema) error {
	// TODO: Implement code generation
	return nil
}
