
## Phase 2: Code Generation Engine
**Duration**: 4 weeks
**Goal**: Generate Go code from parsed TL schema

---

### Task 2.1: Go Type Name Mapper
**Agent**: CodeGen Agent
**Documents**: DESIGN.md naming conventions

**Specifications**:
Create `pkg/codegen/names.go` for consistent Go naming.

**Mapping Rules**:
| TL Name | Go Name | Notes |
|---------|---------|-------|
| `user` | `User` | Constructor → struct |
| `userEmpty` | `UserEmpty` | Constructor → struct |
| `User` | `User` | Type → interface |
| `auth.sendCode` | `Auth_SendCode` | Method → function |
| `send_code` | `SendCode` | Snake → Pascal |
| `reply_to_msg_id` | `ReplyToMsgID` | Snake → Pascal |
| `user_id` | `UserID` | Acronyms uppercase |
| `mtproto.Object` | `mtproto.Object` | Keep namespace |

**Special Cases**:
- Reserved Go keywords: `type` → `Type_`, `range` → `Range_`
- Initialisms: `Id` → `ID`, `Url` → `URL`, `Http` → `HTTP`
- Pluralization: Don't pluralize (keep `Vector` not `Vectors`)

**Interface**:
```go
type Namer struct {
    reserved map[string]bool
}

func (n *Namer) ConstructorName(name string) string
func (n *Namer) TypeName(name string) string
func (n *Namer) MethodName(name string) string // auth.sendCode → SendCode
func (n *Namer) ServiceName(name string) string // auth → AuthServer
func (n *Namer) FieldName(name string) string
func (n *Namer) PackageName(name string) string
```

**Deliverables**:
- `pkg/codegen/names.go` - Name mapping logic
- `pkg/codegen/names_test.go` - Test all mapping rules
- `pkg/codegen/reserved.go` - Go reserved keywords list

**Verification**:
- [ ] All TL names from layer222 map to valid Go identifiers
- [ ] No conflicts with Go keywords
- [ ] Consistent naming across all types

---

### Task 2.2: Type Generator
**Agent**: CodeGen Agent
**Documents**: DESIGN.md generated code structure

**Specifications**:
Create `pkg/codegen/gen_types.go` to generate Go structs from TL types.

**Generated Structure**:
```go
// Input: user#8f97c628 flags:# id:long first_name:flags.0?string = User;

// Output:
type User struct {
    ID        int64
    FirstName *string  // flags.0?string → pointer
    // Flags computed, not stored
}

func (u *User) ConstructorID() uint32 { return 0x8f97c628 }
func (u *User) Method() string        { return "" } // Empty for types
func (u *User) TLName() string        { return "user" }

// Polymorphic types become interfaces
type UserType interface {
    isUserType()
    ConstructorID() uint32
    TLName() string
}

func (*User) isUserType() {}
func (*UserEmpty) isUserType() {}
```

**Type Mapping**:
| TL Type | Go Type | Serialization |
|---------|---------|---------------|
| `int` | `int32` | 4 bytes LE |
| `long` | `int64` | 8 bytes LE |
| `int128` | `[16]byte` | 16 bytes |
| `int256` | `[32]byte` | 32 bytes |
| `double` | `float64` | 8 bytes IEEE 754 |
| `string` | `string` | length + bytes |
| `bytes` | `[]byte` | length + bytes |
| `Bool` | `bool` | int32 0x997275b5 or 0xbc799737 |
| `true` | `bool` | implicit in flags |
| `vector<T>` | `[]T` | int32 count + items |
| `flags` | `uint32` | bitfield |
| `flags.N?T` | `*T` | pointer, omitted if flag not set |

**Generator Interface**:
```go
type TypeGenerator struct {
    namer *Namer
    out   *bytes.Buffer
}

func (g *TypeGenerator) GenerateType(decl *TypeDecl) error
func (g *TypeGenerator) GenerateConstructor(ctor *Constructor) error
```

**Deliverables**:
- `pkg/codegen/gen_types.go` - Type generator
- `pkg/codegen/gen_types_test.go` - Generate and compile test
- `pkg/codegen/template_types.go` - Text templates (optional)

**Verification**:
- [ ] Generates compilable Go code for layer222 types
- [ ] All fields have correct Go types
- [ ] Polymorphic types generate interfaces
- [ ] Conditional fields use pointers

---

### Task 2.3: Service Interface Generator
**Agent**: CodeGen Agent
**Documents**: API.md service definitions

**Specifications**:
Create `pkg/codegen/gen_service.go` for service interfaces.

**Generated Structure**:
```go
// Input: auth.sendCode#a677244f ... = auth.SentCode;

// Output:
type AuthServer interface {
    SendCode(ctx context.Context, req *SendCodeRequest) (*AuthSentCode, error)
    SignIn(ctx context.Context, req *SignInRequest) (*AuthAuthorization, error)
    // ... all auth methods
}

// Unimplemented for embedding
type UnimplementedAuthServer struct{}

func (UnimplementedAuthServer) SendCode(context.Context, *SendCodeRequest) (*AuthSentCode, error) {
    return nil, ErrMethodNotImplemented
}

// Request/Response types (from TL)
type SendCodeRequest struct {
    PhoneNumber string
    APIID       int32
    APIHash     string
    // ...
}

// Registration helper
func RegisterAuthServer(s *tlrpc.Server, srv AuthServer) {
    s.RegisterService(ServiceDesc{
        ServiceName: "auth",
        Methods: []MethodDesc{
            {
                MethodName: "auth.sendCode",
                Handler: func(ctx context.Context, req interface{}) (interface{}, error) {
                    return srv.SendCode(ctx, req.(*SendCodeRequest))
                },
            },
            // ... other methods
        },
    })
}
```

**Generator Interface**:
```go
type ServiceGenerator struct {
    namer *Namer
    out   *bytes.Buffer
}

func (g *ServiceGenerator) GenerateService(funcs []FuncDecl) error
func (g *ServiceGenerator) GenerateRegistration(funcs []FuncDecl) error
```

**Deliverables**:
- `pkg/codegen/gen_service.go` - Service generator
- `pkg/codegen/gen_service_test.go` - Interface compliance tests

**Verification**:
- [ ] Generated interfaces compile
- [ ] Registration functions work with tlrpc.Server
- [ ] Method names match TL function names
- [ ] Context is first parameter in all methods

---

### Task 2.4: File Writer & Organization
**Agent**: CodeGen Agent
**Documents**: Project structure in README.md

**Specifications**:
Create `pkg/codegen/writer.go` to organize generated code into files.

**File Organization**:
```
gen/
├── types.go              # All TL types (constructors)
├── interfaces.go         # Polymorphic type interfaces
├── services.go           # Service interfaces
├── register.go           # Registration functions
├── requests.go           # Request structs (grouped by service)
├── responses.go          # Response structs
└── constants.go          # Constructor IDs as constants
```

**Writer Interface**:
```go
type FileWriter struct {
    outDir    string
    pkgName   string
    files     map[string]*bytes.Buffer
}

func (w *FileWriter) NewFile(name string) io.Writer
func (w *FileWriter) WriteAll() error // Creates files with headers
func (w *FileWriter) Format() error   // gofmt all files
```

**File Header Template**:
```go
// Code generated by tlrpc-gen. DO NOT EDIT.
// Source: {{.SchemaFile}}
// Layer: {{.Layer}}

package {{.PackageName}}

import (
    "context"
    "github.com/r6m/tlrpc/pkg/tlrpc"
)
```

**Deliverables**:
- `pkg/codegen/writer.go` - File writer
- `pkg/codegen/writer_test.go` - File creation tests

**Verification**:
- [ ] Creates valid Go package structure
- [ ] All files have proper headers
- [ ] Files are gofmt'd
- [ ] Package compiles without errors

---

### Task 2.5: CLI Integration
**Agent**: CLI Agent
**Documents**: cmd/tlrpc-gen/main.go specification

**Specifications**:
Complete `cmd/tlrpc-gen/main.go` integrating all components.

**Features**:
- Parse command-line flags (defined in main.go spec)
- Read TL schema file
- Parse with validation
- Generate all code files
- Format output
- Verbose logging option

**Error Handling**:
- Exit code 0 on success
- Exit code 1 on parse error (with file:line info)
- Exit code 2 on generation error
- Exit code 3 on IO error

**Progress Output**:
```
$ tlrpc-gen --schema=layer222.tl --out=./gen --verbose
Parsing schema: layer222.tl...
Found 1,247 constructors, 342 functions
Generating types... 1,247 generated
Generating services... 342 methods across 12 services
Writing files to ./gen...
Formatting with gofmt...
Done. Generated 7 files in ./gen
```

**Deliverables**:
- `cmd/tlrpc-gen/main.go` - Complete CLI
- `cmd/tlrpc-gen/main_test.go` - Integration tests

**Verification**:
- [ ] CLI runs with all flag combinations
- [ ] Generates code for layer222 that compiles
- [ ] Help text is comprehensive
- [ ] Error messages are actionable

