package codegen

import (
	"fmt"
	"io"
	"sort"
)

// CodecGenerator emits constructor registration helpers.
type CodecGenerator struct {
	namer *Namer
	out   io.Writer
}

// NewCodecGenerator creates a new CodecGenerator.
func NewCodecGenerator(namer *Namer, out io.Writer) *CodecGenerator {
	return &CodecGenerator{namer: namer, out: out}
}

// Generate emits a RegisterCodec helper for the schema.
func (g *CodecGenerator) Generate(schema *Schema) error {
	if _, err := io.WriteString(g.out, "import (\n\t\"github.com/r6m/tlrpc\"\n\t\"github.com/r6m/tlrpc/codec\"\n)\n\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(g.out, "func RegisterCodec(reg *codec.Registry) {\n\tif reg == nil {\n\t\treturn\n\t}\n"); err != nil {
		return err
	}

	constructors := make([]Constructor, 0, len(schema.Constructors))
	for _, ctor := range schema.Constructors {
		if len(ctor.GenericParams) > 0 || ctor.ResultType.IsTypeVar {
			continue
		}
		constructors = append(constructors, ctor)
	}
	sort.Slice(constructors, func(i, j int) bool {
		return constructors[i].ID < constructors[j].ID
	})

	for _, ctor := range constructors {
		name := g.namer.ConstructorName(ctor.Name)
		if _, err := fmt.Fprintf(g.out, "\treg.RegisterConstructor(0x%08x, func() tlrpc.TLObject { return &%s{} })\n", ctor.ID, name); err != nil {
			return err
		}
	}

	services := groupByService(schema.Functions)
	serviceNames := sortedKeys(services)
	for _, service := range serviceNames {
		for _, fn := range services[service] {
			if fn.IsTemplate {
				continue
			}
			method := g.namer.MethodName(fn.Name)
			reqType := method + "Request"
			if _, err := fmt.Fprintf(g.out, "\treg.RegisterConstructor(0x%08x, func() tlrpc.TLObject { return &%s{} })\n", fn.ID, reqType); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(g.out, "\treg.RegisterMethod(%q, func() tlrpc.TLObject { return &%s{} })\n", fn.Name, reqType); err != nil {
				return err
			}
		}
	}

	if _, err := io.WriteString(g.out, "}\n"); err != nil {
		return err
	}
	return nil
}
