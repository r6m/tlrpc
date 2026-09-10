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
	return {{.ZeroValue}}, tlrpc.ErrUnimplemented
}
{{end}}
`

// GenerateService emits service interfaces and unimplemented stubs.
func (g *ServiceGenerator) GenerateService(funcs []parser.FuncDecl) error {
	if err := validateMethodResultLayouts(funcs); err != nil {
		return err
	}
	services := groupByService(funcs)

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
			if fn.IsTemplate || fn.IsHelper {
				continue
			}
			method := methodName(g.namer, fn)
			reqType := requestName(g.namer, fn)
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
	if err := validateMethodResultLayouts(funcs); err != nil {
		return err
	}
	services := groupByService(funcs)

	serviceNames := sortedKeys(services)
	for _, service := range serviceNames {
		if err := g.generateRegistrationFunction(service, services[service]); err != nil {
			return err
		}
	}

	return nil
}

// generateRegistrationFunction emits a static service descriptor and its
// gRPC-like registration helper.
func (g *ServiceGenerator) generateRegistrationFunction(service string, funcs []parser.FuncDecl) error {
	name := g.namer.ServiceName(service)
	descName := strings.TrimSuffix(name, "Server") + "_ServiceDesc"

	var methods []parser.FuncDecl
	for _, fn := range funcs {
		if fn.IsTemplate || fn.IsHelper {
			continue
		}
		methods = append(methods, fn)
	}

	for _, fn := range methods {
		method := methodName(g.namer, fn)
		reqType := requestName(g.namer, fn)
		respType := g.responseType(fn.ResultType)
		handlerName := "_" + strings.TrimSuffix(name, "Server") + "_" + method + "_Handler"
		encoderName := "_" + strings.TrimSuffix(name, "Server") + "_" + method + "_EncodeResponse"

		if _, err := fmt.Fprintf(g.out, "func %s(srv any, ctx context.Context, req tlrpc.TLObject) (any, error) {\n\ttypedRequest, ok := req.(*%s)\n\tif !ok || typedRequest == nil {\n\t\treturn nil, fmt.Errorf(\"%s: request %%T is not *%s\", req)\n\t}\n\treturn srv.(%s).%s(ctx, typedRequest)\n}\n\n", handlerName, reqType, fn.Name, reqType, name, method); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "func %s(e *mtproto.Encoder, response %s) error {\n", encoderName, respType); err != nil {
			return err
		}
		emitter := cursorCodec{out: g.out, schema: g.schema, namer: g.namer, cursor: "e", goBase: g.goBaseType}
		if err := emitter.writeSerializeValue(fn.ResultType, "response", "\t"); err != nil {
			return err
		}
		if _, err := io.WriteString(g.out, "\treturn nil\n}\n\n"); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(g.out, "// %s is the static descriptor for the %s service.\nvar %s = tlrpc.ServiceDesc{\n\tServiceName: %q,\n\tSchemaLayer: SchemaLayer,\n\tHandlerType: (*%s)(nil),\n\tMethods: []tlrpc.MethodDesc{\n", descName, name, descName, service, name); err != nil {
		return err
	}
	for _, fn := range methods {
		method := methodName(g.namer, fn)
		reqType := requestName(g.namer, fn)
		respType := g.responseType(fn.ResultType)
		handlerName := "_" + strings.TrimSuffix(name, "Server") + "_" + method + "_Handler"
		encoderName := "_" + strings.TrimSuffix(name, "Server") + "_" + method + "_EncodeResponse"
		for _, interval := range declarationIntervals(fn.Intervals) {
			if _, err := fmt.Fprintf(g.out, "\t\t{\n\t\t\tMinLayer: %d,\n\t\t\tMaxLayer: %d,\n\t\t\tMethodName: %q,\n\t\t\tConstructorID: 0x%08x,\n\t\t\tNewRequest: func() tlrpc.TLObject { return &%s{} },\n\t\t\tHandler: %s,\n\t\t\tEncodeResponse: func(response any, layer int, limits tlrpc.EncodeLimits) ([]byte, error) {\n\t\t\t\treturn tlrpc.EncodeTypedResponse[%s](response, layer, limits, %s)\n\t\t\t},\n\t\t},\n", interval.MinLayer, interval.MaxLayer, method, fn.ID, reqType, handlerName, respType, encoderName); err != nil {
				return err
			}
		}
	}
	if _, err := io.WriteString(g.out, "\t},\n}\n\n"); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(g.out, "// Register%s registers the %s server with the TLRPC server.\nfunc Register%s(s *tlrpc.Server, srv %s) {\n", name, name, name, name); err != nil {
		return err
	}

	// Add the embedded check like gRPC does
	if _, err := fmt.Fprintf(g.out, "\t// If the following call panics, it indicates Unimplemented%s was\n\t// embedded by pointer and is nil. This will cause panics if an\n\t// unimplemented method is ever invoked, so we test this at initialization\n\t// time to prevent it from happening at runtime later due to I/O.\n\tif t, ok := srv.(interface{ testEmbeddedByValue() }); ok {\n\t\tt.testEmbeddedByValue()\n\t}\n", name); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(g.out, "\ts.RegisterService(%s, srv)\n}\n\n", descName); err != nil {
		return err
	}

	return nil
}

// GenerateRequests emits request structs for functions.
func (g *ServiceGenerator) GenerateRequests(funcs []parser.FuncDecl) error {
	if err := validateMethodResultLayouts(funcs); err != nil {
		return err
	}
	services := groupByService(funcs)
	serviceNames := sortedKeys(services)
	emitted := make(map[string]struct{})
	for _, service := range serviceNames {
		for _, fn := range services[service] {
			if fn.IsTemplate || fn.IsHelper {
				continue
			}
			reqName := requestName(g.namer, fn)
			if _, exists := emitted[reqName]; exists {
				continue
			}
			emitted[reqName] = struct{}{}
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

			if err := g.generateRequestMethods(fn, reqName); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *ServiceGenerator) generateRequestMethods(fn parser.FuncDecl, reqName string) error {
	if _, err := fmt.Fprintf(g.out, "func (r *%s) ConstructorID() uint32 { return 0x%08x }\n", reqName, fn.ID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "func (r *%s) Method() string { return %q }\n\n", reqName, fn.Name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "func (r *%s) TLName() string { return %q }\n\n", reqName, fn.Name); err != nil {
		return err
	}
	if hasFlagsParam(fn.Params) {
		if err := g.generateRequestComputeFlags(fn, reqName); err != nil {
			return err
		}
	}

	if err := g.generateRequestSerialize(fn, reqName); err != nil {
		return err
	}
	if err := g.generateRequestDeserialize(fn, reqName); err != nil {
		return err
	}
	return nil
}

func (g *ServiceGenerator) generateRequestComputeFlags(fn parser.FuncDecl, reqName string) error {
	if _, err := fmt.Fprintf(g.out, "func (r *%s) computeFlags() uint32 {\n\tvar flags uint32\n", reqName); err != nil {
		return err
	}
	for _, param := range fn.Params {
		if isFlagsParam(param) || param.FlagBit == nil || flagSetName(param) != "flags" {
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

func (g *ServiceGenerator) responseType(t parser.TypeRef) string {
	base := g.goBaseType(t)
	if g.shouldPointerReturn(t) {
		return "*" + base
	}
	return base
}

func (g *ServiceGenerator) shouldPointerReturn(t parser.TypeRef) bool {
	if isUnionType(g.schema, t) {
		return false
	}
	return shouldUsePointerForType(g.schema, t)
}

func (g *ServiceGenerator) goType(t parser.TypeRef) string {
	base := g.goBaseType(t)
	if isBoxedSingletonPointer(g.schema, t) {
		return "*" + base
	}
	if t.Optional && !isTrueType(t) && !isUnionType(g.schema, t) && !strings.HasPrefix(base, "[]") {
		return "*" + base
	}
	return base
}

func isFlagsParam(param parser.Parameter) bool {
	return isFlagParam(param)
}

func hasFlagsParam(params []parser.Parameter) bool {
	for _, param := range params {
		if isFlagsParam(param) {
			return true
		}
	}
	return false
}

func validateMethodResultLayouts(functions []parser.FuncDecl) error {
	for _, function := range functions {
		if function.IsTemplate || function.IsHelper {
			continue
		}
		if containsBareReference(function.ResultType) {
			return fmt.Errorf("generate method %s: bare result layouts are unsupported by the fixed response encoder", function.Name)
		}
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
	if name, ok := referenceConcreteTypeName(g.namer, g.schema, t); ok {
		return name
	}

	if t.Namespace != "" {
		return typeName(g.namer, t.Namespace+"."+t.Name, t.VariantLayer)
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
		return typeName(g.namer, t.Name, t.VariantLayer)
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
	case "int32", "int64", "uint32", "uint64", "float64", "Double":
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
			if services[name][i].Name == services[name][j].Name {
				return services[name][i].VariantLayer < services[name][j].VariantLayer
			}
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
