package parser

import (
	"fmt"
	"strings"
)

// String returns a string representation of the schema.
func (s *Schema) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Schema{Layer: %d", s.Layer)

	if len(s.Types) > 0 {
		b.WriteString(", Types: [")
		for i, typ := range s.Types {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(typ.String())
		}
		b.WriteString("]")
	}

	if len(s.Functions) > 0 {
		b.WriteString(", Functions: [")
		for i, fn := range s.Functions {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(fn.String())
		}
		b.WriteString("]")
	}

	b.WriteString("}")
	return b.String()
}

// String returns a string representation of the type declaration.
func (t TypeDecl) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "TypeDecl{Name: %q, IsUnion: %v, Constructors: [", t.Name, t.IsUnion)
	for i, ctor := range t.Constructors {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(ctor.String())
	}
	b.WriteString("]}")
	return b.String()
}

// String returns a string representation of the constructor.
func (c Constructor) String() string {
	var b strings.Builder
	if c.IsBare {
		b.WriteString("%")
	}
	fmt.Fprintf(&b, "Constructor{Name: %q, ID: 0x%08x", c.Name, c.ID)

	if len(c.Params) > 0 {
		b.WriteString(", Params: [")
		for i, param := range c.Params {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(param.String())
		}
		b.WriteString("]")
	}

	fmt.Fprintf(&b, ", ResultType: %s}", c.ResultType.String())
	return b.String()
}

// String returns a string representation of the function declaration.
func (f FuncDecl) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "FuncDecl{Name: %q, ID: 0x%08x", f.Name, f.ID)

	if len(f.Params) > 0 {
		b.WriteString(", Params: [")
		for i, param := range f.Params {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(param.String())
		}
		b.WriteString("]")
	}

	fmt.Fprintf(&b, ", ResultType: %s}", f.ResultType.String())
	return b.String()
}

// String returns a string representation of the parameter.
func (p Parameter) String() string {
	if p.FlagBit != nil {
		return fmt.Sprintf("Parameter{Name: %q, Type: %s, FlagBit: %d}", p.Name, p.Type.String(), *p.FlagBit)
	}
	return fmt.Sprintf("Parameter{Name: %q, Type: %s}", p.Name, p.Type.String())
}
