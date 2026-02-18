package generator

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/template"

	"github.com/r6m/tlrpc/internal/naming"
	"github.com/r6m/tlrpc/internal/parser"
)

// ServiceGenerator generates service interfaces and registrations.
type ServiceGenerator struct {
	namer  *naming.Namer
	schema *parser.Schema
	out    io.Writer
}

// NewServiceGenerator creates a new service generator.
func NewServiceGenerator(namer *naming.Namer, schema *parser.Schema, out io.Writer) *ServiceGenerator {
	return &ServiceGenerator{namer: namer, schema: schema, out: out}
}

// ServiceTemplateData holds data for service template
type ServiceTemplateData struct {
	Name        string
	Methods     []MethodTemplateData
	StubMethods []StubMethodTemplateData
}

// MethodTemplateData holds data for interface methods
type MethodTemplateData struct {
	Name     string
	ReqType  string
	RespType string
}

// StubMethodTemplateData holds data for unimplemented stub methods
type StubMethodTemplateData struct {
	ServiceName string
	Name        string
	ReqType     string
	RespType    string
	ZeroValue   string
}

// serviceInterfaceTemplate generates a service interface
const serviceInterfaceTemplate = `type {{.Name}} interface {
{{- range .Methods}}
	{{.Name}}(ctx context.Context, req *{{.ReqType}}) ({{.RespType}}, error)
{{- end}}
}

type Unimplemented{{.Name}} struct{}

func (Unimplemented{{.Name}}) testEmbeddedByValue() {}
{{range .StubMethods}}
func (Unimplemented{{$.Name}}) {{.Name}}(context.Context, *{{.ReqType}}) ({{.RespType}}, error) {
	return {{.ZeroValue}}, ErrMethodNotImplemented
}
{{end}}
`

// GenerateService emits service interfaces and unimplemented stubs.
func (g *ServiceGenerator) GenerateService(funcs []parser.FuncDecl) error {
	services := groupByService(funcs)
	if _, err := io.WriteString(g.out, "var ErrMethodNotImplemented = errors.New(\"tlrpc: method not implemented\")\n\n"); err != nil {
		return err
	}

	serviceNames := sortedKeys(services)
	tmpl, err := template.New("service").Funcs(templateFuncMap()).Parse(serviceInterfaceTemplate)
	if err != nil {
		return err
	}

	for _, service := range serviceNames {
		name := g.namer.ServiceName(service)

		var methods []MethodTemplateData
		var stubMethods []StubMethodTemplateData

		for _, fn := range services[service] {
			if fn.IsTemplate {
				continue
			}
			method := g.namer.MethodName(fn.Name)
			reqType := g.namer.RequestName(fn.Name)
			respType := g.responseType(fn.ResultType)

			methods = append(methods, MethodTemplateData{
				Name:     method,
				ReqType:  reqType,
				RespType: respType,
			})

			stubMethods = append(stubMethods, StubMethodTemplateData{
				ServiceName: name,
				Name:        method,
				ReqType:     reqType,
				RespType:    respType,
				ZeroValue:   zeroValue(respType),
			})
		}

		data := ServiceTemplateData{
			Name:        name,
			Methods:     methods,
			StubMethods: stubMethods,
		}

		if err := tmpl.Execute(g.out, data); err != nil {
			return err
		}
	}

	return nil
}

// GenerateRegistration emits static service descriptors and registration helpers (gRPC-like pattern).
func (g *ServiceGenerator) GenerateRegistration(funcs []parser.FuncDecl) error {
	services := groupByService(funcs)

	serviceNames := sortedKeys(services)
	for _, service := range serviceNames {
		if err := g.generateRegistrationFunction(service, services[service]); err != nil {
			return err
		}
	}

	return nil
}

// generateRegistrationFunction emits the simplified registration function.
func (g *ServiceGenerator) generateRegistrationFunction(service string, funcs []parser.FuncDecl) error {
	name := g.namer.ServiceName(service)

	if _, err := fmt.Fprintf(g.out, "// Register%s registers the %s server with the TLRPC server.\nfunc Register%s(s *tlrpc.Server, srv %s) {\n", name, name, name, name); err != nil {
		return err
	}

	// Add the embedded check like gRPC does
	if _, err := fmt.Fprintf(g.out, "\t// If the following call panics, it indicates Unimplemented%s was\n\t// embedded by pointer and is nil. This will cause panics if an\n\t// unimplemented method is ever invoked, so we test this at initialization\n\t// time to prevent it from happening at runtime later due to I/O.\n\tif t, ok := srv.(interface{ testEmbeddedByValue() }); ok {\n\t\tt.testEmbeddedByValue()\n\t}\n", name); err != nil {
		return err
	}

	for _, fn := range funcs {
		if fn.IsTemplate {
			continue
		}
		method := g.namer.MethodName(fn.Name)
		reqType := g.namer.RequestName(fn.Name)
		if _, err := fmt.Fprintf(g.out, "\ts.RegisterConstructor(0x%08x, func() tlrpc.TLObject { return &%s{} })\n", fn.ID, reqType); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "\ts.RegisterMethod(0x%08x, func(ctx context.Context, obj tlrpc.TLObject) (interface{}, error) {\n\t\treturn srv.%s(ctx, obj.(*%s))\n\t})\n", fn.ID, method, reqType); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(g.out, "}\n\n"); err != nil {
		return err
	}

	return nil
}

// GenerateRequests emits request structs for functions.
func (g *ServiceGenerator) GenerateRequests(funcs []parser.FuncDecl) error {
	services := groupByService(funcs)
	serviceNames := sortedKeys(services)
	for _, service := range serviceNames {
		for _, fn := range services[service] {
			if fn.IsTemplate {
				continue
			}
			reqName := g.namer.RequestName(fn.Name)
			if _, err := fmt.Fprintf(g.out, "type %s struct {\n", reqName); err != nil {
				return err
			}
			for _, param := range fn.Params {
				if shouldSkipParam(param) {
					continue
				}
				fieldName := g.namer.FieldName(param.Name)
				fieldType := g.goType(param.Type)
				if _, err := fmt.Fprintf(g.out, "\t%s %s\n", fieldName, fieldType); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(g.out, "}\n\n"); err != nil {
				return err
			}

			if err := g.generateRequestMethods(fn, strings.TrimSuffix(reqName, "Request")); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *ServiceGenerator) generateRequestMethods(fn parser.FuncDecl, reqBaseName string) error {
	if _, err := fmt.Fprintf(g.out, "func (r *%sRequest) ConstructorID() uint32 { return 0x%08x }\n", reqBaseName, fn.ID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "func (r *%sRequest) Method() string { return %q }\n\n", reqBaseName, fn.Name); err != nil {
		return err
	}
	if hasFlagsParam(fn.Params) {
		if err := g.generateRequestComputeFlags(fn, reqBaseName); err != nil {
			return err
		}
	}

	if err := g.generateRequestSerialize(fn, reqBaseName); err != nil {
		return err
	}
	if err := g.generateRequestDeserialize(fn, reqBaseName); err != nil {
		return err
	}
	return nil
}

func (g *ServiceGenerator) generateRequestComputeFlags(fn parser.FuncDecl, reqBaseName string) error {
	if _, err := fmt.Fprintf(g.out, "func (r *%sRequest) computeFlags() uint32 {\n\tvar flags uint32\n", reqBaseName); err != nil {
		return err
	}
	for _, param := range fn.Params {
		if isFlagsParam(param) || param.FlagBit == nil {
			continue
		}
		fieldName := g.namer.FieldName(param.Name)
		if isTrueType(param.Type) {
			if _, err := fmt.Fprintf(g.out, "\tif r.%s {\n\t\tflags |= 1 << %d\n\t}\n", fieldName, *param.FlagBit); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(g.out, "\tif r.%s != nil {\n\t\tflags |= 1 << %d\n\t}\n", fieldName, *param.FlagBit); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(g.out, "\treturn flags\n}\n\n"); err != nil {
		return err
	}
	return nil
}

func (g *ServiceGenerator) generateRequestSerialize(fn parser.FuncDecl, reqBaseName string) error {
	if _, err := fmt.Fprintf(g.out, "func (r *%sRequest) SerializeTL(w io.Writer) error {\n", reqBaseName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "\tif err := mtproto.WriteUint32(w, r.ConstructorID()); err != nil {\n\t\treturn err\n\t}\n"); err != nil {
		return err
	}
	if hasFlagsParam(fn.Params) {
		if _, err := io.WriteString(g.out, "\tflags := r.computeFlags()\n"); err != nil {
			return err
		}
	}
	for _, param := range fn.Params {
		if isFlagsParam(param) {
			if _, err := io.WriteString(g.out, "\tif err := mtproto.WriteUint32(w, flags); err != nil {\n\t\treturn err\n\t}\n"); err != nil {
				return err
			}
			continue
		}
		fieldName := g.namer.FieldName(param.Name)
		if param.FlagBit != nil {
			if _, err := fmt.Fprintf(g.out, "\tif flags&(1<<%d) != 0 {\n", *param.FlagBit); err != nil {
				return err
			}
			if err := g.writeRequestSerializeField(param, fieldName, "\t\t"); err != nil {
				return err
			}
			if _, err := io.WriteString(g.out, "\t}\n"); err != nil {
				return err
			}
			continue
		}
		if err := g.writeRequestSerializeField(param, fieldName, "\t"); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(g.out, "\treturn nil\n}\n\n"); err != nil {
		return err
	}
	return nil
}

func (g *ServiceGenerator) writeRequestSerializeField(param parser.Parameter, fieldName, indent string) error {
	typeRef := param.Type
	if typeRef.IsVector && typeRef.Generic != nil {
		if _, err := fmt.Fprintf(g.out, "%sif err := mtproto.WriteVectorHeader(w, len(r.%s)); err != nil {\n%s\treturn err\n%s}\n", indent, fieldName, indent, indent); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%sfor i := range r.%s {\n", indent, fieldName); err != nil {
			return err
		}
		if err := g.writeRequestSerializeValue(*typeRef.Generic, fmt.Sprintf("r.%s[i]", fieldName), indent+"\t"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s}\n", indent); err != nil {
			return err
		}
		return nil
	}
	value := "r." + fieldName
	if needsOptionalPointer(typeRef, g.goBaseType(typeRef), g.schema) {
		if _, ok := serializeBuiltinCall(typeRef, value); ok {
			value = "*" + value
		}
	}
	return g.writeRequestSerializeValue(typeRef, value, indent)
}

func (g *ServiceGenerator) writeRequestSerializeValue(t parser.TypeRef, value, indent string) error {
	writeCall, ok := serializeBuiltinCall(t, value)
	if ok {
		_, err := fmt.Fprintf(g.out, "%s%s\n", indent, writeCall)
		return err
	}
	_, err := fmt.Fprintf(g.out, "%sif err := %s.SerializeTL(w); err != nil {\n%s\treturn err\n%s}\n", indent, value, indent, indent)
	return err
}

func (g *ServiceGenerator) generateRequestDeserialize(fn parser.FuncDecl, reqBaseName string) error {
	if _, err := fmt.Fprintf(g.out, "func (r *%sRequest) DeserializeTL(rd io.Reader) error {\n", reqBaseName); err != nil {
		return err
	}
	if _, err := io.WriteString(g.out, "\tctorID, err := mtproto.ReadUint32(rd)\n\tif err != nil {\n\t\treturn err\n\t}\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "\tif ctorID != r.ConstructorID() {\n\t\treturn fmt.Errorf(\"wrong constructor: got %%x, want %%x\", ctorID, r.ConstructorID())\n\t}\n"); err != nil {
		return err
	}
	if hasFlagsParam(fn.Params) {
		if _, err := io.WriteString(g.out, "\tvar flags uint32\n"); err != nil {
			return err
		}
	}
	for _, param := range fn.Params {
		if isFlagsParam(param) {
			if _, err := io.WriteString(g.out, "\t{\n\t\tvalue, err := mtproto.ReadUint32(rd)\n\t\tif err != nil {\n\t\t\treturn err\n\t\t}\n\t\tflags = value\n\t}\n"); err != nil {
				return err
			}
			continue
		}
		fieldName := g.namer.FieldName(param.Name)
		if param.FlagBit != nil {
			if _, err := fmt.Fprintf(g.out, "\tif flags&(1<<%d) != 0 {\n", *param.FlagBit); err != nil {
				return err
			}
			if err := g.writeRequestDeserializeField(param, fieldName, "\t\t"); err != nil {
				return err
			}
			if _, err := io.WriteString(g.out, "\t}\n"); err != nil {
				return err
			}
			continue
		}
		if err := g.writeRequestDeserializeField(param, fieldName, "\t"); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(g.out, "\treturn nil\n}\n\n"); err != nil {
		return err
	}
	return nil
}

func (g *ServiceGenerator) writeRequestDeserializeField(param parser.Parameter, fieldName, indent string) error {
	typeRef := param.Type
	if typeRef.IsVector && typeRef.Generic != nil {
		elementBase := g.goBaseTypeNonVector(*typeRef.Generic)
		elementType := elementBase
		elementPtr := shouldUsePointerForType(g.schema, *typeRef.Generic)
		if elementPtr {
			elementType = "*" + elementBase
		}
		if _, err := fmt.Fprintf(g.out, "%s{\n", indent); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s\tvar items []%s\n", indent, elementType); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s\tif err := mtproto.ReadVector(rd, func() error {\n", indent); err != nil {
			return err
		}
		if elementPtr {
			if _, err := fmt.Fprintf(g.out, "%s\t\titem := &%s{}\n", indent, elementBase); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintf(g.out, "%s\t\tvar item %s\n", indent, elementType); err != nil {
				return err
			}
		}
		if err := g.writeRequestDeserializeValue(*typeRef.Generic, "item", indent+"\t\t"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s\t\titems = append(items, item)\n%s\t\treturn nil\n%s\t}); err != nil {\n%s\t\treturn err\n%s\t}\n", indent, indent, indent, indent, indent); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s\tr.%s = items\n", indent, fieldName); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s}\n", indent); err != nil {
			return err
		}
		return nil
	}
	base := g.goBaseType(typeRef)
	if needsOptionalPointer(typeRef, base, g.schema) {
		return g.writeRequestDeserializeOptionalPointer(typeRef, base, "r."+fieldName, indent)
	}
	return g.writeRequestDeserializeValue(typeRef, "r."+fieldName, indent)
}

func (g *ServiceGenerator) writeRequestDeserializeValue(t parser.TypeRef, target, indent string) error {
	readCall, ok := deserializeBuiltinCallWithReader(t, "rd")
	if ok {
		if _, err := fmt.Fprintf(g.out, "%s{\n%s\tvalue, err := %s\n%s\tif err != nil {\n%s\t\treturn err\n%s\t}\n", indent, indent, readCall, indent, indent, indent); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s\t%s = value\n%s}\n", indent, target, indent); err != nil {
			return err
		}
		return nil
	}
	if _, err := fmt.Fprintf(g.out, "%sif err := %s.DeserializeTL(rd); err != nil {\n%s\treturn err\n%s}\n", indent, target, indent, indent); err != nil {
		return err
	}
	return nil
}

func (g *ServiceGenerator) responseType(t parser.TypeRef) string {
	base := g.goBaseType(t)
	if g.shouldPointerReturn(t) {
		return "*" + base
	}
	return base
}

func (g *ServiceGenerator) shouldPointerReturn(t parser.TypeRef) bool {
	return shouldUsePointerForType(g.schema, t)
}

func (g *ServiceGenerator) goType(t parser.TypeRef) string {
	base := g.goBaseType(t)
	if t.Optional && !isTrueType(t) && !isUnionType(g.schema, t) && !strings.HasPrefix(base, "[]") {
		return "*" + base
	}
	return base
}

func isFlagsParam(param parser.Parameter) bool {
	return param.Name == "flags" && param.Type.Name == "#"
}

func hasFlagsParam(params []parser.Parameter) bool {
	for _, param := range params {
		if isFlagsParam(param) {
			return true
		}
	}
	return false
}

func (g *ServiceGenerator) writeRequestDeserializeOptionalPointer(t parser.TypeRef, baseType, target, indent string) error {
	readCall, ok := deserializeBuiltinCallWithReader(t, "rd")
	if ok {
		if _, err := fmt.Fprintf(g.out, "%svalue, err := %s\n%sif err != nil {\n%s\treturn err\n%s}\n", indent, readCall, indent, indent, indent); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s%s = &value\n", indent, target); err != nil {
			return err
		}
		return nil
	}
	if _, err := fmt.Fprintf(g.out, "%svar value %s\n", indent, baseType); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "%sif err := value.DeserializeTL(rd); err != nil {\n%s\treturn err\n%s}\n", indent, indent, indent); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "%s%s = &value\n", indent, target); err != nil {
		return err
	}
	return nil
}

func (g *ServiceGenerator) goBaseType(t parser.TypeRef) string {
	if t.IsVector && t.Generic != nil {
		elementBase := g.goBaseTypeNonVector(*t.Generic)
		if shouldUsePointerForType(g.schema, *t.Generic) {
			return "[]*" + elementBase
		}
		return "[]" + elementBase
	}
	return g.goBaseTypeNonVector(t)
}

func (g *ServiceGenerator) goBaseTypeNonVector(t parser.TypeRef) string {
	if t.IsVector && t.Generic != nil {
		elementBase := g.goBaseTypeNonVector(*t.Generic)
		if shouldUsePointerForType(g.schema, *t.Generic) {
			return "[]*" + elementBase
		}
		return "[]" + elementBase
	}

	if isUnionType(g.schema, t) {
		return unionInterfaceName(g.namer, t)
	}

	if t.Namespace != "" {
		return g.namer.TypeName(t.Namespace + "." + t.Name)
	}

	switch t.Name {
	case "int":
		return "int32"
	case "long":
		return "int64"
	case "int128":
		return "Int128"
	case "int256":
		return "Int256"
	case "double":
		return "Double"
	case "string":
		return "string"
	case "bytes":
		return "[]byte"
	case "true", "false":
		return "bool"
	case "error":
		return "Error"
	case "null":
		return "Null"
	case "vector":
		if t.Generic != nil {
			return "Vector[" + g.goBaseType(*t.Generic) + "]"
		}
		return "Vector[interface{}]"
	case "Bool", "bool":
		return "bool"
	case "#":
		return "uint32"
	default:
		return g.namer.TypeName(t.Name)
	}
}

func zeroValue(typ string) string {
	if strings.HasPrefix(typ, "*") {
		return "nil"
	}
	if strings.HasSuffix(typ, "Type") {
		return "nil"
	}
	switch typ {
	case "string":
		return "\"\""
	case "bool":
		return "false"
	case "int32", "int64", "uint32", "uint64", "float64":
		return "0"
	default:
		if strings.HasPrefix(typ, "[]") {
			return "nil"
		}
		return typ + "{}"
	}
}

func groupByService(funcs []parser.FuncDecl) map[string][]parser.FuncDecl {
	services := make(map[string][]parser.FuncDecl)
	for _, fn := range funcs {
		parts := strings.Split(fn.Name, ".")
		service := ""
		if len(parts) > 1 {
			service = parts[0]
		} else {
			service = "root"
		}
		services[service] = append(services[service], fn)
	}
	for name := range services {
		sort.Slice(services[name], func(i, j int) bool {
			return services[name][i].Name < services[name][j].Name
		})
	}
	return services
}

func sortedKeys(m map[string][]parser.FuncDecl) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
