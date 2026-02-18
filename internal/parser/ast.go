package parser

import "fmt"

// Position represents a position in the source code.
type Position struct {
	Line   int
	Column int
}

// Schema is the root node of a TL schema AST.
type Schema struct {
	Layer        int             // Detected or provided layer version
	Types        []TypeDecl      // From ---types--- section
	Functions    []FuncDecl      // From ---functions--- section
	Constructors []Constructor   // All constructors from types
	UnionTypes   map[string]bool // Types that are unions (have multiple constructors)
}

// GenericParam represents a generic type parameter like {t:Type}.
type GenericParam struct {
	Name       string // "t", "X"
	Constraint string // "Type", "Int", etc.
	Pos        Position
}

// TypeDecl represents a type declaration with all its constructors.
type TypeDecl struct {
	Name         string        // e.g., "User", "Message"
	Constructors []Constructor // All constructors for this type
	IsUnion      bool          // true if multiple constructors
}

// Constructor represents a single constructor in TL.
type Constructor struct {
	Name          string         // e.g., "user", "userEmpty"
	ID            uint32         // Hex ID or computed CRC32
	GenericParams []GenericParam // NEW: {t:Type}
	Params        []Parameter
	ResultType    TypeRef // Return type
	IsBare        bool    // % prefix
	VectorCount   *string // NEW: element variable for vectors, e.g., "t" in "# [ t ]"
}

// FuncDecl represents a function declaration.
type FuncDecl struct {
	Name          string // e.g., "auth.sendCode"
	ID            uint32
	GenericParams []GenericParam // NEW: {X:Type}
	Params        []Parameter
	ResultType    TypeRef
	IsTemplate    bool // NEW: true if return type is generic param (e.g., = X)
}

// Parameter represents a parameter in a constructor or function.
type Parameter struct {
	Name    string
	Type    TypeRef
	FlagBit *int // nil if not conditional
}

// TypeRef represents a type reference, possibly generic or conditional.
type TypeRef struct {
	Name       string // "int", "long", "User", etc.
	Namespace  string // "mtproto" in "mtproto.Object"
	IsVector   bool
	IsBare     bool     // ! prefix for bare types
	Generic    *TypeRef // For vector<Generic>
	GenericArg string   // NEW: for "Vector t" - the "t" part
	Optional   bool     // flags.N?Type
	FlagBit    *int     // Conditional on this flag bit
	IsTypeVar  bool     // NEW: true if this is a type variable like "t" or "X"
}

// NewTypeRef creates a simple type reference.
func NewTypeRef(name string) TypeRef {
	return TypeRef{Name: name}
}

// NewNamespacedTypeRef creates a namespaced type reference.
func NewNamespacedTypeRef(namespace, name string) TypeRef {
	return TypeRef{
		Name:      name,
		Namespace: namespace,
	}
}

// NewVectorTypeRef creates a vector type reference.
func NewVectorTypeRef(elementType TypeRef) TypeRef {
	return TypeRef{
		Name:     "vector",
		IsVector: true,
		Generic:  &elementType,
	}
}

// NewBareTypeRef creates a bare type reference.
func NewBareTypeRef(name string) TypeRef {
	return TypeRef{
		Name:   name,
		IsBare: true,
	}
}

// NewOptionalTypeRef creates an optional type reference.
func NewOptionalTypeRef(base TypeRef) TypeRef {
	return TypeRef{
		Name:     base.Name,
		Optional: true,
	}
}

// String returns a string representation of the type reference.
func (t TypeRef) String() string {
	var result string

	if t.IsBare {
		result += "!"
	}

	if t.FlagBit != nil {
		result += fmt.Sprintf("flags.%d?", *t.FlagBit)
	}

	if t.Namespace != "" {
		result += t.Namespace + "."
	}

	result += t.Name

	// Handle generic argument like "Vector t"
	if t.GenericArg != "" {
		result += " " + t.GenericArg
	}

	if t.IsVector {
		result += "<"
		if t.Generic != nil {
			result += t.Generic.String()
		}
		result += ">"
	}

	if t.Optional && t.FlagBit == nil {
		result += "?"
	}

	return result
}

// FullName returns the full qualified name of the type.
func (t TypeRef) FullName() string {
	if t.Namespace != "" {
		return t.Namespace + "." + t.Name
	}
	return t.Name
}

// IsBuiltin returns true if this is a built-in TL type.
func (t TypeRef) IsBuiltin() bool {
	switch t.Name {
	case "int", "long", "int128", "int256", "double", "string", "bytes",
		"bool", "true", "false", "Bool", "Object", "Function", "Type", "#":
		return true
	default:
		return false
	}
}

// isTypeVariable returns true if the given name is a type variable (single letter).
func isTypeVariable(name string) bool {
	if len(name) != 1 {
		return false
	}
	ch := name[0]
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

// String returns a string representation of a generic parameter.
func (p GenericParam) String() string {
	return fmt.Sprintf("%s:%s", p.Name, p.Constraint)
}

// NewSchema creates a new schema with the given layer.
func NewSchema(layer int) *Schema {
	return &Schema{
		Layer:        layer,
		Types:        []TypeDecl{},
		Functions:    []FuncDecl{},
		Constructors: []Constructor{},
		UnionTypes:   make(map[string]bool),
	}
}

// AddType adds a type declaration to the schema.
func (s *Schema) AddType(typ TypeDecl) {
	s.Types = append(s.Types, typ)
	if typ.IsUnion {
		s.UnionTypes[typ.Name] = true
	}
	for _, ctor := range typ.Constructors {
		s.Constructors = append(s.Constructors, ctor)
	}
}

// AddFunction adds a function declaration to the schema.
func (s *Schema) AddFunction(fn FuncDecl) {
	s.Functions = append(s.Functions, fn)
}

// FindType finds a type by name.
func (s *Schema) FindType(name string) (*TypeDecl, bool) {
	for _, typ := range s.Types {
		if typ.Name == name {
			return &typ, true
		}
	}
	return nil, false
}

// FindConstructor finds a constructor by ID.
func (s *Schema) FindConstructor(id uint32) (*Constructor, bool) {
	for _, ctor := range s.Constructors {
		if ctor.ID == id {
			return &ctor, true
		}
	}
	return nil, false
}

// FindFunction finds a function by ID.
func (s *Schema) FindFunction(id uint32) (*FuncDecl, bool) {
	for _, fn := range s.Functions {
		if fn.ID == id {
			return &fn, true
		}
	}
	return nil, false
}
