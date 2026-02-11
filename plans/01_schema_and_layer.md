## Phase 1: TL Schema Parser
**Duration**: 3 weeks
**Goal**: Parse Telegram TL schema files into structured AST

---

### Task 1.1: TL Tokenizer/Lexer
**Agent**: Parser Agent
**Documents**: TL grammar in ARCHITECTURE.md, DESIGN.md

**Specifications**:
Create `internal/codegen/lexer.go` that tokenizes TL schema syntax.

**Token Types**:
```go
type TokenType int

const (
    TokenEOF TokenType = iota
    TokenIdent      // user, messages.sendMessage
    TokenNumber     // 123, 0x520c3870
    TokenColon      // :
    TokenSemi       // ;
    TokenEquals     // =
    TokenLParen     // (
    TokenRParen     // )
    TokenLBrace     // {
    TokenRBrace     // }
    TokenLBracket   // [
    TokenRBracket   // ]
    TokenLess       // <
    TokenGreater    // >
    TokenQuestion   // ?
    TokenHash       // #
    TokenBang       // !
    TokenPercent    // %
    TokenDot        // .
    TokenUnderscore // _
    TokenNewLine    // \n
    TokenComment    // // comment
    TokenTypes      // ---types---
    TokenFunctions  // ---functions---
)
```

**Lexer Interface**:
```go
type Lexer struct {
    input string
    pos   int
    line  int
    col   int
}

func NewLexer(input string) *Lexer
func (l *Lexer) NextToken() Token
func (l *Lexer) PeekToken() Token
```

**Handling**:
- Line comments starting with `//`
- Section markers `---types---` and `---functions---`
- Hex numbers (constructor IDs)
- Identifiers with dots (namespaces)
- Whitespace is significant for separation but skipped between tokens

**Deliverables**:
- `internal/codegen/lexer.go` - Lexer implementation
- `internal/codegen/token.go` - Token definitions
- `internal/codegen/lexer_test.go` - Unit tests with 100% coverage

**Verification**:
- [ ] Tokenizes `layer222.tl` without errors
- [ ] All token types tested
- [ ] Handles edge cases (empty input, invalid chars)
- [ ] Benchmark: >1MB/s tokenization speed

---

### Task 1.2: TL Parser AST
**Agent**: Parser Agent
**Documents**: AST design in ARCHITECTURE.md

**Specifications**:
Create `internal/codegen/ast.go` defining the Abstract Syntax Tree types.

**AST Nodes**:
```go
// Schema is the root node
type Schema struct {
    Layer        int          // Detected or provided layer version
    Types        []TypeDecl   // From ---types--- section
    Functions    []FuncDecl   // From ---functions--- section
    Constructors []Constructor // All constructors from types
}

type TypeDecl struct {
    Name         string       // e.g., "User", "Message"
    Constructors []Constructor // All constructors for this type
    IsUnion      bool         // true if multiple constructors
}

type Constructor struct {
    Name       string      // e.g., "user", "userEmpty"
    ID         uint32      // Hex ID or computed CRC32
    Params     []Parameter
    ResultType TypeRef     // Return type
    IsBare     bool        // % prefix
}

type FuncDecl struct {
    Name       string      // e.g., "auth.sendCode"
    ID         uint32
    Params     []Parameter
    ResultType TypeRef
}

type Parameter struct {
    Name     string
    Type     TypeRef
    FlagBit  *int       // nil if not conditional
}

type TypeRef struct {
    Name      string     // "int", "long", "User", etc.
    Namespace string     // "mtproto" in "mtproto.Object"
    IsVector  bool
    IsBare    bool
    Generic   *TypeRef   // For vector<Generic>
    Optional  bool       // flags.N?Type
}
```

**Deliverables**:
- `internal/codegen/ast.go` - All AST types
- `internal/codegen/ast_test.go` - Test AST construction
- `internal/codegen/ast_string.go` - String() methods for debugging

**Verification**:
- [ ] All AST types can be constructed
- [ ] String representations are readable
- [ ] Handles recursive types (vector<vector<int>>)

---

### Task 1.3: Recursive Descent Parser
**Agent**: Parser Agent
**Documents**: TL grammar in ARCHITECTURE.md

**Specifications**:
Create `internal/codegen/parser.go` implementing recursive descent parser.

**Grammar Rules** (from ARCHITECTURE.md):
```
schema := section*

section := typesSection | functionsSection

typesSection := "---types---" constructor*

functionsSection := "---functions---" funcDecl*

constructor := ident "#" hexId params? "=" typeRef ";"
            | "%" ident params? "=" typeRef ";"  // bare type

funcDecl := ident "#" hexId params? "=" typeRef ";"

params := "{" param ("," param)* "}"

param := ident ":" typeRef
       | "flags" "." number "?" typeRef  // conditional

typeRef := ident
         | ident "<" typeRef ">"         // generic
         | "%" typeRef                   // bare
         | "!" typeRef                   // bare function type

hexId := "0x" hexDigits | computed automatically
```

**Parser Interface**:
```go
type Parser struct {
    lexer *Lexer
    cur   Token
    peek  Token
}

func NewParser(input string) *Parser
func (p *Parser) Parse() (*Schema, error)
func (p *Parser) ParseWithLayer(input string, layer int) (*Schema, error)
```

**Error Handling**:
- Line and column numbers in errors
- Context in error messages (show surrounding tokens)
- Continue parsing after error (collect multiple errors)

**CRC32 Computation**:
For constructors without explicit ID, compute CRC32 of serialization format string:
```go
// Format: "constructorName param1:type1 param2:type2 = resultType"
// Example: "user flags:# id:long = User"
func computeCRC32(format string) uint32
```

**Deliverables**:
- `internal/codegen/parser.go` - Parser implementation
- `internal/codegen/parser_test.go` - Comprehensive tests
- `internal/codegen/errors.go` - Error types with context

**Verification**:
- [ ] Parses complete `layer222.tl` (from Telegram desktop repo)
- [ ] All constructor IDs match official values
- [ ] Error messages include line:column information
- [ ] Handles 10,000+ line schema files in <100ms

---

### Task 1.4: Schema Validation
**Agent**: Validation Agent
**Documents**: DESIGN.md validation rules

**Specifications**:
Create `internal/codegen/validate.go` to validate parsed schemas.

**Validation Rules**:
1. **Unique Constructor IDs**: No duplicate IDs in schema
2. **Unique Function IDs**: No duplicate method IDs
3. **Type Resolution**: All referenced types must be defined (built-in or declared)
4. **Flag Consistency**: If `flags:#` present, flag bits must be unique per constructor
5. **Circular Check**: No circular type dependencies (except through vectors)
6. **Namespace Validity**: Valid identifier format for namespaces

**Built-in Types**:
```
int, long, int128, int256, double, string, bytes, 
bool, true, false, Bool, Object, Function, Type, #
```

**Validator Interface**:
```go
type Validator struct {
    schema *Schema
    errors []ValidationError
}

type ValidationError struct {
    Line    int
    Column  int
    Message string
    Severity ErrorSeverity // Warning or Error
}

func (v *Validator) Validate() error // Returns multi-error if any
```

**Deliverables**:
- `internal/codegen/validate.go` - Validator implementation
- `internal/codegen/validate_test.go` - Test cases for each rule
- `internal/codegen/builtin.go` - Built-in type definitions

**Verification**:
- [ ] Detects duplicate constructor IDs
- [ ] Detects undefined types
- [ ] Validates flag bit uniqueness
- [ ] Passes on valid `layer222.tl`
- [ ] Fails with helpful messages on invalid schemas
