## Phase 1: TL Schema Parser (Production-Ready)
**Duration**: 2 weeks
**Goal**: Parse complete Telegram TL schema including generic types
**Status**: Core validated, extending for full schema compatibility

---

### Task 1.1: Core Lexer (VALIDATED - Extend for Generics)
**Agent**: Parser Agent  
**Documents**: `internal/tlcodegen/lexer.go`, real schema test results

**Current State**: ✅ Core syntax working (identifiers, numbers, symbols, comments, section markers)

**Required Extensions for Generic Syntax**:

Add these token types to `internal/tlcodegen/token.go`:

```go
const (
    // Existing tokens...
    TokenEOF TokenType = iota
    TokenIdent
    TokenNumber
    // ... all existing tokens ...
    
    // NEW: Generic syntax tokens
    TokenLBraceCurly    // { for generic params
    TokenRBraceCurly    // } for generic params
    TokenLBracket       // [ for vector count
    TokenRBracket       // ] for vector count
)
```

**Lexer Extensions in `internal/tlcodegen/lexer.go`**:

```go
func (l *Lexer) NextToken() Token {
    // ... existing switch cases ...
    
    switch l.ch {
    // ... existing cases ...
    
    case '{':
        tok = newToken(TokenLBraceCurly, l.ch)
    case '}':
        tok = newToken(TokenRBraceCurly, l.ch)
    case '[':
        tok = newToken(TokenLBracket, l.ch)
    case ']':
        tok = newToken(TokenRBracket, l.ch)
        
    // Handle '# [' sequence specially for vector count
    case '#':
        if l.peekChar() == ' ' && l.peekCharN(2) == '[' {
            // This is vector count syntax: # [ t ]
            tok.Literal = "# ["
            tok.Type = TokenHashBracket
            l.readChar() // skip space
            l.readChar() // skip [
        } else {
            tok = newToken(TokenHash, l.ch)
        }
    }
    
    // ... rest of function ...
}
```

**Deliverables**:
- [ ] Updated `internal/tlcodegen/token.go` with new token types
- [ ] Updated `internal/tlcodegen/lexer.go` handling `{ } [ ]`
- [ ] Special handling for `# [` vector count syntax
- [ ] `internal/tlcodegen/lexer_test.go` tests for:
  - `{t:Type}` tokenization
  - `# [ t ]` tokenization  
  - Mixed generic and regular syntax
- [ ] Benchmark: >1MB/s on `schema.tl` (validated)

**Verification**:
```go
func TestGenericTokens(t *testing.T) {
    input := `vector#1cb5c415 {t:Type} # [ t ] = Vector t;`
    l := NewLexer(input)
    
    tests := []struct {
        expectedType    TokenType
        expectedLiteral string
    }{
        {TokenIdent, "vector"},
        {TokenHash, "#"},
        {TokenNumber, "1cb5c415"},
        {TokenLBraceCurly, "{"},
        {TokenIdent, "t"},
        {TokenColon, ":"},
        {TokenIdent, "Type"},
        {TokenRBraceCurly, "}"},
        {TokenHashBracket, "# ["}, // or separate tokens, your choice
        {TokenIdent, "t"},
        {TokenRBracket, "]"},
        // ... rest
    }
    
    for i, tt := range tests {
        tok := l.NextToken()
        assert.Equal(t, tt.expectedType, tok.Type, "test %d", i)
        assert.Equal(t, tt.expectedLiteral, tok.Literal, "test %d", i)
    }
}
```

---

### Task 1.2: AST for Generic Types (NEW)
**Agent**: Parser Agent  
**Documents**: Real schema analysis, `internal/tlcodegen/ast.go`

**Extend AST types in `internal/tlcodegen/ast.go`**:

```go
// Schema is the root node
type Schema struct {
    Layer        int
    Types        []TypeDecl
    Functions    []FuncDecl
    Constructors []Constructor
}

// Generic parameter: {t:Type} or {X:Type}
type GenericParam struct {
    Name string // "t", "X"
    Constraint string // "Type", "Int", etc.
    Pos  Position
}

type Constructor struct {
    Name        string
    ID          uint32
    GenericParams []GenericParam // NEW: {t:Type}
    Params      []Parameter
    ResultType  TypeRef
    IsBare      bool
    VectorCount *string // NEW: element variable for vectors, e.g., "t" in "# [ t ]"
}

type FuncDecl struct {
    Name        string
    ID          uint32
    GenericParams []GenericParam // NEW: {X:Type}
    Params      []Parameter
    ResultType  TypeRef
    IsTemplate  bool // NEW: true if return type is generic param (e.g., = X)
}

// Extended TypeRef for generics
type TypeRef struct {
    Name       string
    Namespace  string
    IsVector   bool
    IsBare     bool
    Generic    *TypeRef       // For vector<T>
    GenericArg string         // NEW: for "Vector t" - the "t" part
    Optional   bool
    IsTypeVar  bool           // NEW: true if this is a type variable like "t" or "X"
}
```

**Deliverables**:
- [ ] Updated `internal/tlcodegen/ast.go` with generic support
- [ ] `internal/tlcodegen/ast_test.go` with generic construction tests
- [ ] String() methods updated for debugging

---

### Task 1.3: Parser with Generic Support (MERGED)
**Agent**: Parser Agent  
**Documents**: Grammar rules below, `internal/tlcodegen/parser.go`

**Complete Grammar** (core + generics):

```
schema := section*

section := typesSection | functionsSection

typesSection := "---types---" constructor*

functionsSection := "---functions---" funcDecl*

// Constructor with optional generic params and vector count
constructor := ident "#" hexId genericParams? vectorCount? params? "=" typeRef ";"
            | "%" ident genericParams? params? "=" typeRef ";"  // bare type

// Function with optional generic params
funcDecl := ident "#" hexId genericParams? params? "=" typeRef ";"

// Generic parameters: {t:Type, X:Type}
genericParams := "{" genericParam ("," genericParam)* "}"
genericParam  := ident ":" typeRef

// Vector element count: # [ t ]
vectorCount := "#" "[" ident "]"

params := "{" param ("," param)* "}"

param := ident ":" typeRef
       | "flags" "." number "?" typeRef  // conditional

// Type reference with generic arguments
typeRef := ident typeArg?           // Vector t, or just int
         | ident "<" typeRef ">"    // vector<int>
         | "%" typeRef              // bare
         | "!" typeRef              // bare function type

typeArg := ident                    // the "t" in "Vector t"
```

**Parser Implementation Strategy**:

Extend existing recursive descent parser in `internal/tlcodegen/parser.go`:

```go
func (p *Parser) parseConstructor() (*Constructor, error) {
    ctor := &Constructor{}
    
    // Parse name
    ctor.Name = p.curToken.Literal
    
    // Expect #
    if !p.expectPeek(TokenHash) {
        return nil, fmt.Errorf("expected # after constructor name")
    }
    
    // Parse ID
    if !p.expectPeek(TokenNumber) {
        return nil, fmt.Errorf("expected constructor ID")
    }
    id, err := strconv.ParseUint(p.curToken.Literal, 16, 32)
    if err != nil {
        return nil, fmt.Errorf("invalid hex ID: %v", err)
    }
    ctor.ID = uint32(id)
    
    // NEW: Parse optional generic params {t:Type}
    if p.peekTokenIs(TokenLBraceCurly) {
        p.nextToken()
        params, err := p.parseGenericParams()
        if err != nil {
            return nil, err
        }
        ctor.GenericParams = params
    }
    
    // NEW: Parse optional vector count # [ t ]
    if p.peekTokenIs(TokenHash) && p.peekTokenN(2).Type == TokenLBracket {
        p.nextToken() // consume #
        p.nextToken() // consume [
        if !p.curTokenIs(TokenLBracket) {
            return nil, fmt.Errorf("expected [ after # for vector count")
        }
        if !p.expectPeek(TokenIdent) {
            return nil, fmt.Errorf("expected element variable name")
        }
        ctor.VectorCount = &p.curToken.Literal
        if !p.expectPeek(TokenRBracket) {
            return nil, fmt.Errorf("expected ] after element variable")
        }
    }
    
    // Parse params (existing logic)
    if p.peekTokenIs(TokenLBrace) {
        p.nextToken()
        params, err := p.parseParams()
        if err != nil {
            return nil, err
        }
        ctor.Params = params
    }
    
    // Expect =
    if !p.expectPeek(TokenEquals) {
        return nil, fmt.Errorf("expected =")
    }
    
    // Parse result type
    p.nextToken()
    resultType, err := p.parseTypeRef()
    if err != nil {
        return nil, err
    }
    ctor.ResultType = *resultType
    
    // Expect ;
    if !p.expectPeek(TokenSemi) {
        return nil, fmt.Errorf("expected ;")
    }
    
    return ctor, nil
}

// NEW: Parse generic parameters
func (p *Parser) parseGenericParams() ([]GenericParam, error) {
    var params []GenericParam
    
    // Current token is {
    for !p.peekTokenIs(TokenRBraceCurly) {
        p.nextToken()
        
        // Parse name
        if !p.curTokenIs(TokenIdent) {
            return nil, fmt.Errorf("expected identifier in generic param")
        }
        param := GenericParam{Name: p.curToken.Literal}
        
        // Expect :
        if !p.expectPeek(TokenColon) {
            return nil, fmt.Errorf("expected : in generic param")
        }
        
        // Parse constraint type
        p.nextToken()
        typeRef, err := p.parseTypeRef()
        if err != nil {
            return nil, err
        }
        param.Constraint = typeRef.Name
        
        params = append(params, param)
        
        // Check for comma or end
        if p.peekTokenIs(TokenComma) {
            p.nextToken()
        }
    }
    
    // Consume }
    if !p.expectPeek(TokenRBraceCurly) {
        return nil, fmt.Errorf("expected } to close generic params")
    }
    
    return params, nil
}

// UPDATED: Parse type reference with generic args
func (p *Parser) parseTypeRef() (*TypeRef, error) {
    ref := &TypeRef{}
    
    // Handle bare prefix
    if p.curTokenIs(TokenPercent) || p.curTokenIs(TokenBang) {
        ref.IsBare = true
        p.nextToken()
    }
    
    // Type name
    if !p.curTokenIs(TokenIdent) {
        return nil, fmt.Errorf("expected type name, got %s", p.curToken.Type)
    }
    ref.Name = p.curToken.Literal
    
    // Check for namespace
    if p.peekTokenIs(TokenDot) {
        ref.Namespace = ref.Name
        p.nextToken() // consume .
        p.nextToken() // consume ident
        ref.Name = p.curToken.Literal
    }
    
    // Check for generic arg: "Vector t" (bare identifier after type)
    if p.peekTokenIs(TokenIdent) {
        // This could be a generic argument like "t" in "Vector t"
        // OR it could be the next parameter name
        // Heuristic: if next token is not : or ., it's likely generic arg
        peek := p.peekToken()
        if peek.Type != TokenColon && peek.Type != TokenDot {
            p.nextToken()
            ref.GenericArg = p.curToken.Literal
            ref.IsTypeVar = isTypeVariable(ref.GenericArg) // check if lowercase
        }
    }
    
    // Check for <T> generic syntax
    if p.peekTokenIs(TokenLess) {
        p.nextToken()
        p.nextToken()
        generic, err := p.parseTypeRef()
        if err != nil {
            return nil, err
        }
        ref.Generic = generic
        if !p.expectPeek(TokenGreater) {
            return nil, fmt.Errorf("expected > to close generic")
        }
    }
    
    return ref, nil
}
```

**CRC32 Computation** (for constructors without explicit ID):

```go
func computeCRC32(format string) uint32 {
    return crc32.ChecksumIEEE([]byte(format))
}

// Build format string from constructor
func (c *Constructor) FormatString() string {
    var parts []string
    parts = append(parts, c.Name)
    
    for _, p := range c.Params {
        parts = append(parts, fmt.Sprintf("%s:%s", p.Name, p.Type.String()))
    }
    
    parts = append(parts, "=", c.ResultType.String())
    return strings.Join(parts, " ")
}
```

**Deliverables**:
- [ ] Updated `internal/tlcodegen/parser.go` with generic support
- [ ] `internal/tlcodegen/parser_test.go` with:
  - Core parsing tests (from old spec)
  - Generic constructor tests (vector, invokeAfterMsg)
  - Function template tests (invokeWithLayer)
  - Error recovery tests
- [ ] `internal/tlcodegen/crc32.go` for ID computation
- [ ] `internal/tlcodegen/errors.go` with line:column context

**Verification**:
```go
func TestParseVector(t *testing.T) {
    input := `vector#1cb5c415 {t:Type} # [ t ] = Vector t;`
    p := NewParser(input)
    schema, err := p.Parse()
    require.NoError(t, err)
    
    require.Len(t, schema.Constructors, 1)
    v := schema.Constructors[0]
    
    assert.Equal(t, "vector", v.Name)
    assert.Equal(t, uint32(0x1cb5c415), v.ID)
    require.Len(t, v.GenericParams, 1)
    assert.Equal(t, "t", v.GenericParams[0].Name)
    assert.Equal(t, "Type", v.GenericParams[0].Constraint)
    require.NotNil(t, v.VectorCount)
    assert.Equal(t, "t", *v.VectorCount)
    assert.Equal(t, "Vector", v.ResultType.Name)
    assert.Equal(t, "t", v.ResultType.GenericArg)
}

func TestParseInvokeWithLayer(t *testing.T) {
    input := `invokeWithLayer#da9b0d0d {X:Type} layer:int query:!X = X;`
    p := NewParser(input)
    schema, err := p.Parse()
    require.NoError(t, err)
    
    require.Len(t, schema.Functions, 1)
    f := schema.Functions[0]
    
    assert.Equal(t, "invokeWithLayer", f.Name)
    require.Len(t, f.GenericParams, 1)
    assert.Equal(t, "X", f.GenericParams[0].Name)
    assert.True(t, f.IsTemplate) // returns X
}
```

---

### Task 1.4: Real Schema Validation Suite (MERGED)
**Agent**: QA Agent  
**Documents**: Test results, `testdata/schema.tl`

**Test File: `internal/tlcodegen/real_schema_test.go`**:

```go
package tlcodegen

import (
    "os"
    "testing"
    
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestRealSchema(t *testing.T) {
    // Download schema if not present
    schemaPath := "../../testdata/schema.tl"
    if _, err := os.Stat(schemaPath); os.IsNotExist(err) {
        t.Skip("schema.tl not found, run: make download-schema")
    }
    
    data, err := os.ReadFile(schemaPath)
    require.NoError(t, err)
    
    p := NewParser(string(data))
    schema, err := p.Parse()
    require.NoError(t, err, "Failed to parse real schema: %v", err)
    
    // Validate substantial content
    assert.Greater(t, len(schema.Constructors), 1000, "Should have many constructors")
    assert.Greater(t, len(schema.Functions), 300, "Should have many functions")
    
    // Validate specific critical constructors
    assert.True(t, hasConstructor(schema, "user"), "Should have user constructor")
    assert.True(t, hasConstructor(schema, "message"), "Should have message constructor")
    assert.True(t, hasConstructor(schema, "vector"), "Should have vector constructor")
    
    // Validate specific critical functions
    assert.True(t, hasFunction(schema, "auth.sendCode"), "Should have auth.sendCode")
    assert.True(t, hasFunction(schema, "invokeWithLayer"), "Should have invokeWithLayer")
    
    // Validate generic types parsed correctly
    vector := findConstructor(schema, "vector")
    require.NotNil(t, vector, "vector constructor should exist")
    assert.Len(t, vector.GenericParams, 1, "vector should have generic param")
    assert.Equal(t, "t", vector.GenericParams[0].Name)
    assert.Equal(t, "Type", vector.GenericParams[0].Constraint)
    
    // Validate vector count syntax
    require.NotNil(t, vector.VectorCount)
    assert.Equal(t, "t", *vector.VectorCount)
    
    // Validate template function
    invoke := findFunction(schema, "invokeWithLayer")
    require.NotNil(t, invoke)
    assert.Len(t, invoke.GenericParams, 1)
    assert.Equal(t, "X", invoke.GenericParams[0].Name)
    assert.True(t, invoke.IsTemplate)
}

func TestSchemaValidation(t *testing.T) {
    data, err := os.ReadFile("../../testdata/schema.tl")
    require.NoError(t, err)
    
    p := NewParser(string(data))
    schema, err := p.Parse()
    require.NoError(t, err)
    
    v := NewValidator(schema)
    err = v.Validate()
    assert.NoError(t, err, "Real schema should validate")
}

// Helper functions
func hasConstructor(s *Schema, name string) bool {
    for _, c := range s.Constructors {
        if c.Name == name {
            return true
        }
    }
    return false
}

func findConstructor(s *Schema, name string) *Constructor {
    for _, c := range s.Constructors {
        if c.Name == name {
            return c
        }
    }
    return nil
}

func hasFunction(s *Schema, name string) bool {
    for _, f := range s.Functions {
        if f.Name == name {
            return true
        }
    }
    return false
}

func findFunction(s *Schema, name string) *FuncDecl {
    for _, f := range s.Functions {
        if f.Name == name {
            return f
        }
    }
    return nil
}
```

**Makefile Target**:

```makefile
download-schema:
	mkdir -p testdata
	curl -o testdata/schema.tl https://raw.githubusercontent.com/telegramdesktop/tdesktop/dev/Telegram/SourceFiles/mtproto/scheme/layer222.tl

test-real-schema: download-schema
	go test -v ./internal/tlcodegen -run TestRealSchema
```

**Deliverables**:
- [ ] `internal/tlcodegen/real_schema_test.go`
- [ ] `testdata/schema.tl` (downloaded via make)
- [ ] Updated `Makefile` with download-schema target
- [ ] CI job that validates against latest schema nightly

---

### Task 1.5: Schema Validation (EXTENDED)
**Agent**: Validation Agent  
**Documents**: `internal/tlcodegen/validate.go`

**Validation Rules** (all from old spec, plus generics):

1. **Unique Constructor IDs**: No duplicates
2. **Unique Function IDs**: No duplicates  
3. **Type Resolution**: All types defined (built-in or declared)
4. **Flag Consistency**: Unique flag bits per constructor
5. **Circular Check**: No circular dependencies
6. **Namespace Validity**: Valid identifiers
7. **NEW: Generic Param Resolution**: Generic params must be declared before use
8. **NEW: Type Variable Scope**: Type variables (t, X) only valid in generic context

**Extended Validator**:

```go
type Validator struct {
    schema *Schema
    errors []ValidationError
    
    // Track generic params in scope
    genericStack []map[string]bool
}

func (v *Validator) Validate() error {
    v.validateConstructors()
    v.validateFunctions()
    v.validateTypes()
    return v.errorsToError()
}

func (v *Validator) validateTypeRef(ref TypeRef, ctx string) {
    // Check if it's a type variable in scope
    if ref.IsTypeVar {
        if !v.isTypeVarInScope(ref.Name) {
            v.addError("undefined type variable: %s in %s", ref.Name, ctx)
        }
        return
    }
    
    // Existing type resolution logic...
}
```

**Deliverables**:
- [ ] Updated `internal/tlcodegen/validate.go` with generic support
- [ ] `internal/tlcodegen/validate_test.go` with generic validation tests

---

## Summary: Phase 1 Deliverables

| File | Status | Description |
|------|--------|-------------|
| `internal/tlcodegen/token.go` | 🔄 EXTEND | Add `{ } [ ]` tokens |
| `internal/tlcodegen/lexer.go` | 🔄 EXTEND | Generic syntax tokenization |
| `internal/tlcodegen/ast.go` | 🔄 EXTEND | GenericParam, VectorCount, IsTemplate |
| `internal/tlcodegen/parser.go` | 🔄 EXTEND | parseGenericParams, parseVectorCount |
| `internal/tlcodegen/crc32.go` | ✅ KEEP | ID computation |
| `internal/tlcodegen/errors.go` | ✅ KEEP | Error types |
| `internal/tlcodegen/validate.go` | 🔄 EXTEND | Generic validation |
| `internal/tlcodegen/*_test.go` | 🔄 EXTEND | Real schema tests |
| `testdata/schema.tl` | 📥 NEW | Official Telegram schema |
| `Makefile` | 🔄 EXTEND | download-schema target |

**Key Metrics**:
- Parse `schema.tl` (1000+ constructors, 300+ functions) without errors
- Handle all generic syntax: `{t:Type}`, `# [ t ]`, `= X`
- <100ms parse time for full schema
- 100% test coverage on lexer, >90% on parser