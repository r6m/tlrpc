package codegen

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// ServiceGenerator generates service interfaces and registrations.
type ServiceGenerator struct {
	namer *Namer
	out   io.Writer
}

// NewServiceGenerator creates a new service generator.
func NewServiceGenerator(namer *Namer, out io.Writer) *ServiceGenerator {
	return &ServiceGenerator{namer: namer, out: out}
}

// GenerateService emits service interfaces and unimplemented stubs.
func (g *ServiceGenerator) GenerateService(funcs []FuncDecl) error {
	services := groupByService(funcs)
	if _, err := io.WriteString(g.out, "import (\n\t\"context\"\n\t\"errors\"\n)\n\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(g.out, "var ErrMethodNotImplemented = errors.New(\"tlrpc: method not implemented\")\n\n"); err != nil {
		return err
	}

	serviceNames := sortedKeys(services)
	for _, service := range serviceNames {
		name := g.namer.ServiceName(service)
		if _, err := fmt.Fprintf(g.out, "type %s interface {\n", name); err != nil {
			return err
		}
		for _, fn := range services[service] {
			if fn.IsTemplate {
				continue
			}
			method := g.namer.MethodName(fn.Name)
			reqType := method + "Request"
			respType := g.responseType(fn.ResultType)
			if _, err := fmt.Fprintf(g.out, "\t%s(ctx context.Context, req *%s) (%s, error)\n", method, reqType, respType); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(g.out, "}\n\n"); err != nil {
			return err
		}

		if _, err := fmt.Fprintf(g.out, "type Unimplemented%s struct{}\n\n", name); err != nil {
			return err
		}
		for _, fn := range services[service] {
			if fn.IsTemplate {
				continue
			}
			method := g.namer.MethodName(fn.Name)
			reqType := method + "Request"
			respType := g.responseType(fn.ResultType)
			if _, err := fmt.Fprintf(g.out, "func (Unimplemented%s) %s(context.Context, *%s) (%s, error) {\n\treturn %s, ErrMethodNotImplemented\n}\n\n", name, method, reqType, respType, zeroValue(respType)); err != nil {
				return err
			}
		}
	}

	return nil
}

// GenerateRegistration emits registration helpers for services.
func (g *ServiceGenerator) GenerateRegistration(funcs []FuncDecl) error {
	services := groupByService(funcs)
	if _, err := io.WriteString(g.out, "import (\n\t\"context\"\n\t\"github.com/r6m/tlrpc\"\n)\n\n"); err != nil {
		return err
	}

	serviceNames := sortedKeys(services)
	for _, service := range serviceNames {
		name := g.namer.ServiceName(service)
		if _, err := fmt.Fprintf(g.out, "func Register%s(s *tlrpc.Server, srv %s) {\n", name, name); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "\ts.RegisterService(tlrpc.ServiceDesc{\n\t\tServiceName: %q,\n\t\tHandlerType: (*%s)(nil),\n\t\tMethods: []tlrpc.MethodDesc{\n", service, name); err != nil {
			return err
		}
		for _, fn := range services[service] {
			if fn.IsTemplate {
				continue
			}
			method := g.namer.MethodName(fn.Name)
			reqType := method + "Request"
			if _, err := fmt.Fprintf(g.out, "\t\t\t{\n\t\t\t\tMethodName: %q,\n\t\t\t\tHandler: func(ctx context.Context, req interface{}) (interface{}, error) {\n\t\t\t\t\treturn srv.%s(ctx, req.(*%s))\n\t\t\t\t},\n\t\t\t},\n", fn.Name, method, reqType); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(g.out, "\t\t},\n\t})\n}\n\n"); err != nil {
			return err
		}
	}

	return nil
}

// GenerateRequests emits request structs for functions.
func (g *ServiceGenerator) GenerateRequests(funcs []FuncDecl) error {
	services := groupByService(funcs)
	serviceNames := sortedKeys(services)
	for _, service := range serviceNames {
		for _, fn := range services[service] {
			if fn.IsTemplate {
				continue
			}
			method := g.namer.MethodName(fn.Name)
			if _, err := fmt.Fprintf(g.out, "type %sRequest struct {\n", method); err != nil {
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
		}
	}
	return nil
}

func (g *ServiceGenerator) responseType(t TypeRef) string {
	base := g.goBaseType(t)
	if shouldPointerReturn(t) {
		return "*" + base
	}
	return base
}

func shouldPointerReturn(t TypeRef) bool {
	if t.IsVector {
		return false
	}
	if t.Namespace != "" {
		return true
	}
	switch t.Name {
	case "int", "long", "int128", "int256", "double", "string", "bytes", "Bool", "bool", "true", "false", "#":
		return false
	default:
		return true
	}
}

func (g *ServiceGenerator) goType(t TypeRef) string {
	base := g.goBaseType(t)
	if t.Optional && !isTrueType(t) {
		return "*" + base
	}
	return base
}

func (g *ServiceGenerator) goBaseType(t TypeRef) string {
	if t.IsVector && t.Generic != nil {
		return "[]" + g.goBaseType(*t.Generic)
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
		return "[16]byte"
	case "int256":
		return "[32]byte"
	case "double":
		return "float64"
	case "string":
		return "string"
	case "bytes":
		return "[]byte"
	case "Bool", "bool", "true", "false":
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

func groupByService(funcs []FuncDecl) map[string][]FuncDecl {
	services := make(map[string][]FuncDecl)
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

func sortedKeys(m map[string][]FuncDecl) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
