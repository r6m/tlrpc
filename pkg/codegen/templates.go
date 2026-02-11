// Package codegen provides templates for code generation.
package codegen

// Templates contains code generation templates.
type Templates struct {
	TypeTemplate     string
	ServiceTemplate  string
	RegisterTemplate string
}

// NewTemplates creates default templates.
func NewTemplates() *Templates {
	return &Templates{
		TypeTemplate: `// {{.Name}} represents {{.Name}}
type {{.PascalName}} struct {
{{range .Fields}}	{{.PascalName}} {{.GoType}} ` + "`" + `json:"{{.Name}}"` + "`" + `
{{end}}}

func (o *{{.PascalName}}) ConstructorID() uint32 {
	return {{.ConstructorID}}
}

func (o *{{.PascalName}}) Method() string {
	return ""
}
`,

		ServiceTemplate: `// {{.PascalName}}Server represents the {{.Name}} service
type {{.PascalName}}Server interface {
{{range .Methods}}	{{.PascalName}}({{.Params}}) ({{.ResultType}}, error)
{{end}}}

// Unimplemented{{.PascalName}}Server provides default implementations
type Unimplemented{{.PascalName}}Server struct{}

{{range .Methods}}func (s *Unimplemented{{.PascalName}}Server) {{.PascalName}}({{.Params}}) ({{.ResultType}}, error) {
	return nil, errors.New("method not implemented")
}
{{end}}
`,

		RegisterTemplate: `// Register{{.PascalName}}Server registers the server implementation
func Register{{.PascalName}}Server(s *tlrpc.Server, srv {{.PascalName}}Server) {
	// TODO: Implement registration
}
`,
	}
}