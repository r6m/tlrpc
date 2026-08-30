package parser

import (
	"fmt"
	"hash/crc32"
	"strconv"
	"strings"
)

// Parser implements a recursive descent parser for TL schema.
type Parser struct {
	lexer *Lexer
	cur   Token
	peek  Token
}

// NewParser creates a new parser for the given input.
func NewParser(input string) *Parser {
	lexer := NewLexer(input)
	return &Parser{
		lexer: lexer,
	}
}

// Parse parses the input and returns a schema AST.
func (p *Parser) Parse() (*Schema, error) {
	return p.ParseWithLayer(0)
}

// ParseWithLayer parses the input with a specific layer version.
func (p *Parser) ParseWithLayer(layer int) (*Schema, error) {
	// Initialize cur and peek
	p.nextToken()
	p.nextToken()

	schema := NewSchema(layer)

	for p.cur.Type != TokenEOF {
		switch p.cur.Type {
		case TokenTypes:
			if err := p.parseTypesSection(schema); err != nil {
				return nil, err
			}
		case TokenFunctions:
			if err := p.parseFunctionsSection(schema); err != nil {
				return nil, err
			}
		default:
			// Assume it's types section if we haven't seen ---functions--- yet
			if err := p.parseTypesSectionWithoutMarker(schema); err != nil {
				return nil, err
			}
		}
	}
	markSerializerPrefixHelpers(schema)

	return schema, nil
}

func markSerializerPrefixHelpers(schema *Schema) {
	byName := make(map[string]*FuncDecl, len(schema.Functions))
	for i := range schema.Functions {
		byName[schema.Functions[i].Name] = &schema.Functions[i]
	}
	for i := range schema.Functions {
		helper := &schema.Functions[i]
		if !strings.HasSuffix(helper.Name, "Prefix") || helper.ResultType.FullName() != "Error" {
			continue
		}
		base := byName[strings.TrimSuffix(helper.Name, "Prefix")]
		if base != nil && base.ID == helper.ID && base.IsTemplate {
			helper.IsHelper = true
		}
	}
}

// parseTypesSection parses a ---types--- section.
func (p *Parser) parseTypesSection(schema *Schema) error {
	p.expect(TokenTypes)
	p.skipNewlines()

	return p.parseConstructors(schema)
}

// parseTypesSectionWithoutMarker parses types without the ---types--- marker.
func (p *Parser) parseTypesSectionWithoutMarker(schema *Schema) error {
	return p.parseConstructors(schema)
}

// parseConstructors parses constructor declarations until a section marker or EOF.
func (p *Parser) parseConstructors(schema *Schema) error {
	for p.cur.Type != TokenEOF && p.cur.Type != TokenFunctions && p.cur.Type != TokenTypes {
		ctor, err := p.parseConstructor()
		if err != nil {
			return err
		}

		// Group constructors by result type
		typeName := ctor.ResultType.FullName()
		var typeDecl *TypeDecl
		for i := range schema.Types {
			if schema.Types[i].Name == typeName {
				typeDecl = &schema.Types[i]
				break
			}
		}

		if typeDecl == nil {
			typeDecl = &TypeDecl{
				Name:         typeName,
				Constructors: []Constructor{},
			}
			schema.Types = append(schema.Types, *typeDecl)
			typeDecl = &schema.Types[len(schema.Types)-1]
		}

		typeDecl.Constructors = append(typeDecl.Constructors, *ctor)
		typeDecl.IsUnion = len(typeDecl.Constructors) > 1

		// Add to global constructors list
		schema.Constructors = append(schema.Constructors, *ctor)

		if p.cur.Type == TokenSemi {
			p.nextToken()
			p.skipNewlines()
		} else {
			break
		}
	}

	return nil
}

// parseGenericParams parses generic parameters like {t:Type, X:Type}.
func (p *Parser) parseGenericParams() ([]GenericParam, error) {
	var params []GenericParam

	if p.cur.Type != TokenLBrace {
		return nil, p.errorf("expected { to start generic params")
	}
	p.nextToken() // consume {

	for p.cur.Type != TokenRBrace && p.cur.Type != TokenEOF {
		if p.cur.Type != TokenIdent {
			return nil, p.errorf("expected identifier in generic param")
		}
		param := GenericParam{
			Name: p.cur.Literal,
			Pos:  Position{Line: p.cur.Line, Column: p.cur.Column},
		}
		p.nextToken() // consume name

		if p.cur.Type != TokenColon {
			return nil, p.errorf("expected : in generic param")
		}
		p.nextToken() // consume :

		// Parse constraint type (simple identifier for now)
		if p.cur.Type != TokenIdent {
			return nil, p.errorf("expected type constraint in generic param")
		}
		param.Constraint = p.cur.Literal
		p.nextToken() // consume constraint

		params = append(params, param)

		// Check for comma or end
		if p.cur.Type == TokenComma {
			p.nextToken() // consume ,
		} else if p.cur.Type != TokenRBrace {
			return nil, p.errorf("expected , or } in generic params")
		}
	}

	if p.cur.Type != TokenRBrace {
		return nil, p.errorf("expected } to close generic params")
	}
	p.nextToken() // consume }

	return params, nil
}

// parseFunctionsSection parses a ---functions--- section.
func (p *Parser) parseFunctionsSection(schema *Schema) error {
	p.expect(TokenFunctions)
	p.skipNewlines()

	for p.cur.Type != TokenEOF && p.cur.Type != TokenTypes && p.cur.Type != TokenFunctions {
		fn, err := p.parseFunction()
		if err != nil {
			return err
		}

		schema.AddFunction(*fn)

		p.expect(TokenSemi)
		p.skipNewlines()
	}

	return nil
}

// parseConstructor parses a constructor declaration.
func (p *Parser) parseConstructor() (*Constructor, error) {
	var isBare bool
	if p.cur.Type == TokenPercent {
		isBare = true
		p.nextToken()
	}

	name, err := p.parseIdent()
	if err != nil {
		return nil, err
	}

	if p.cur.Type != TokenHash {
		if !isBare {
			if ctor, ok, err := p.parseBuiltinConstructor(name); ok || err != nil {
				return ctor, err
			}
		}
		return nil, p.errorf("expected # after constructor name")
	}
	p.nextToken() // consume #

	var id uint32
	if p.cur.Type == TokenNumber || (p.cur.Type == TokenIdent && isAllHexDigits(p.cur.Literal)) {
		id, err = p.parseHexID()
		if err != nil {
			return nil, err
		}
	} else {
		// No explicit ID, will be computed later
		id = 0 // Placeholder
	}

	// NEW: Parse optional generic params {t:Type}
	var genericParams []GenericParam
	if p.cur.Type == TokenLBrace {
		genericParams, err = p.parseGenericParams()
		if err != nil {
			return nil, err
		}
	}

	// NEW: Parse optional vector count # [ t ]
	var vectorCount *string
	if p.cur.Type == TokenHashBracket {
		p.nextToken() // consume # [
		if p.cur.Type != TokenIdent {
			return nil, p.errorf("expected element variable name after # [")
		}
		count := p.cur.Literal
		vectorCount = &count
		p.nextToken() // consume ident
		if p.cur.Type != TokenRBracket {
			return nil, p.errorf("expected ] after element variable")
		}
		p.nextToken() // consume ]
	}

	params, err := p.parseParams()
	if err != nil {
		return nil, err
	}

	if p.cur.Type != TokenEquals {
		return nil, p.errorf("expected = after constructor params")
	}
	p.nextToken() // consume =

	resultType, err := p.parseTypeRef()
	if err != nil {
		return nil, err
	}

	// Check for generic arg like "Vector t"
	if p.cur.Type == TokenIdent {
		resultType.GenericArg = p.cur.Literal
		resultType.IsTypeVar = isTypeVariable(resultType.GenericArg)
		p.nextToken()
	}

	// If no explicit ID, compute CRC32
	if id == 0 {
		format := p.computeConstructorFormat(name, params, resultType)
		id = computeCRC32(format)
	}

	return &Constructor{
		Name:          name,
		ID:            id,
		GenericParams: genericParams,
		Params:        params,
		ResultType:    resultType,
		IsBare:        isBare,
		VectorCount:   vectorCount,
	}, nil
}

type builtinDeclaration struct {
	result       string
	questionMark bool
}

var builtinDeclarations = map[string]builtinDeclaration{
	"int":    {result: "Int", questionMark: true},
	"long":   {result: "Long", questionMark: true},
	"double": {result: "Double", questionMark: true},
	"string": {result: "String", questionMark: true},
	"bytes":  {result: "Bytes"},
	"int256": {result: "Int256"},
}

// parseBuiltinConstructor parses Telegram's primitive pseudo-declarations,
// such as "int ? = Int" and "bytes = Bytes". These declarations have no
// explicit constructor ID; Telegram derives it from the canonical declaration.
func (p *Parser) parseBuiltinConstructor(name string) (*Constructor, bool, error) {
	declaration, ok := builtinDeclarations[name]
	if !ok {
		return nil, false, nil
	}

	if declaration.questionMark {
		if p.cur.Type != TokenQuestion {
			return nil, true, p.errorf("expected ? after builtin constructor name %s", name)
		}
		p.nextToken()
	} else if p.cur.Type == TokenQuestion {
		return nil, true, p.errorf("unexpected ? after builtin constructor name %s", name)
	}

	if p.cur.Type != TokenEquals {
		return nil, true, p.errorf("expected = after builtin constructor %s", name)
	}
	p.nextToken()

	resultType, err := p.parseTypeRef()
	if err != nil {
		return nil, true, err
	}
	if resultType.FullName() != declaration.result || resultType.Optional || resultType.IsVector || resultType.GenericArg != "" {
		return nil, true, p.errorf("builtin constructor %s must return %s", name, declaration.result)
	}

	format := name + " = " + declaration.result
	if declaration.questionMark {
		format = name + " ? = " + declaration.result
	}

	return &Constructor{
		Name:       name,
		ID:         computeCRC32(format),
		ResultType: resultType,
		IsBuiltin:  true,
	}, true, nil
}

// parseFunction parses a function declaration.
func (p *Parser) parseFunction() (*FuncDecl, error) {
	name, err := p.parseIdent()
	if err != nil {
		return nil, err
	}

	var id uint32
	explicitID := p.cur.Type == TokenHash
	if explicitID {
		p.nextToken() // consume #
		id, err = p.parseHexID()
		if err != nil {
			return nil, err
		}
	}

	// NEW: Parse optional generic params {X:Type}
	var genericParams []GenericParam
	if p.cur.Type == TokenLBrace {
		genericParams, err = p.parseGenericParams()
		if err != nil {
			return nil, err
		}
	}

	params, err := p.parseParams()
	if err != nil {
		return nil, err
	}

	if p.cur.Type != TokenEquals {
		return nil, p.errorf("expected = after function params")
	}
	p.nextToken() // consume =

	resultType, err := p.parseTypeRef()
	if err != nil {
		return nil, err
	}

	// Check for generic arg like "X"
	if p.cur.Type == TokenIdent {
		resultType.GenericArg = p.cur.Literal
		resultType.IsTypeVar = isTypeVariable(resultType.GenericArg)
		p.nextToken()
	}

	if !explicitID {
		id = computeCRC32(p.computeFunctionFormat(name, params, resultType))
	}

	// Check if this is a template function (return type is a generic param)
	isTemplate := false
	for _, gp := range genericParams {
		if resultType.Name == gp.Name {
			isTemplate = true
			break
		}
	}

	return &FuncDecl{
		Name:          name,
		ID:            id,
		GenericParams: genericParams,
		Params:        params,
		ResultType:    resultType,
		IsTemplate:    isTemplate,
	}, nil
}

func (p *Parser) computeFunctionFormat(name string, params []Parameter, resultType TypeRef) string {
	var b strings.Builder
	b.WriteString(name)
	for _, param := range params {
		b.WriteString(" ")
		b.WriteString(param.Name)
		b.WriteString(":")
		b.WriteString(param.Type.String())
	}
	b.WriteString(" = ")
	b.WriteString(resultType.String())
	return b.String()
}

// parseIdent parses an identifier (possibly namespaced), or the special "#" type.
func (p *Parser) parseIdent() (string, error) {
	var parts []string

	if p.cur.Type != TokenIdent && p.cur.Type != TokenHash {
		return "", p.errorf("expected identifier, got %s", p.cur.Type)
	}

	parts = append(parts, p.cur.Literal)
	p.nextToken()

	// Handle namespaced identifiers like "auth.sendCode"
	for p.cur.Type == TokenDot {
		p.nextToken() // consume .
		if p.cur.Type != TokenIdent {
			return "", p.errorf("expected identifier after ., got %s", p.cur.Type)
		}
		parts = append(parts, p.cur.Literal)
		p.nextToken()
	}

	return strings.Join(parts, "."), nil
}

// parseHexID parses a hex ID (with or without 0x prefix).
func (p *Parser) parseHexID() (uint32, error) {
	if p.cur.Type != TokenNumber && p.cur.Type != TokenIdent {
		return 0, p.errorf("expected hex number, got %s", p.cur.Type)
	}

	literal := p.cur.Literal
	p.nextToken()
	if !isAllHexDigits(literal) {
		return 0, p.errorf("expected hex number, got %s", literal)
	}

	// Remove 0x prefix if present
	literal = strings.TrimPrefix(literal, "0x")

	// Parse as hex
	value, err := strconv.ParseUint(literal, 16, 32)
	if err != nil {
		return 0, p.errorf("invalid hex number %s: %v", literal, err)
	}

	return uint32(value), nil
}

// computeCRC32 computes CRC32 of the serialization format string for constructors without explicit IDs.
func computeCRC32(format string) uint32 {
	return crc32.ChecksumIEEE([]byte(format))
}

// computeConstructorFormat creates the serialization format string for CRC32 computation.
func (p *Parser) computeConstructorFormat(name string, params []Parameter, resultType TypeRef) string {
	var b strings.Builder
	b.WriteString(name)

	for _, param := range params {
		b.WriteString(" ")
		b.WriteString(param.Name)
		b.WriteString(":")
		b.WriteString(param.Type.String())
	}

	b.WriteString(" = ")
	b.WriteString(resultType.String())
	b.WriteString(";")

	return b.String()
}

// parseParams parses parameter list (possibly empty).
func (p *Parser) parseParams() ([]Parameter, error) {
	var params []Parameter

	// Parse parameters until we hit "="
	for p.cur.Type != TokenEquals && p.cur.Type != TokenEOF {
		param, err := p.parseParam()
		if err != nil {
			return nil, err
		}
		params = append(params, *param)
	}

	return params, nil
}

// parseParam parses a single parameter.
func (p *Parser) parseParam() (*Parameter, error) {
	// Regular parameter: name:type
	name, err := p.parseIdent()
	if err != nil {
		return nil, err
	}

	if p.cur.Type != TokenColon {
		return nil, p.errorf("expected : in parameter")
	}
	p.nextToken() // consume :

	typeRef, err := p.parseTypeRef()
	if err != nil {
		return nil, err
	}

	param := &Parameter{
		Name: name,
		Type: typeRef,
	}

	// If the type has a flag bit, set it on the parameter
	if typeRef.FlagBit != nil {
		param.FlagBit = typeRef.FlagBit
	}

	return param, nil
}

// parseTypeRef parses a type reference.
func (p *Parser) parseTypeRef() (TypeRef, error) {
	var typeRef TypeRef

	// Handle bare type (!)
	if p.cur.Type == TokenBang {
		typeRef.IsBare = true
		p.nextToken()
	}

	// Check for conditional type: flags.N?Type or flags2.N?Type
	if p.cur.Type == TokenIdent && strings.HasPrefix(p.cur.Literal, "flags") {
		return p.parseConditionalTypeRef()
	}

	// Parse type name (possibly namespaced)
	name, err := p.parseIdent()
	if err != nil {
		return TypeRef{}, err
	}

	// Split namespace and name
	parts := strings.Split(name, ".")
	if len(parts) == 2 {
		typeRef.Namespace = parts[0]
		typeRef.Name = parts[1]
	} else {
		typeRef.Name = name
	}
	typeRef.IsTypeVar = isTypeVariable(typeRef.Name)

	// Handle generic types like vector<Type>
	if p.cur.Type == TokenLess {
		p.nextToken() // consume <
		genericType, err := p.parseTypeRef()
		if err != nil {
			return TypeRef{}, err
		}
		p.expect(TokenGreater) // consume >

		if typeRef.Name == "vector" || typeRef.Name == "Vector" {
			typeRef.IsVector = true
			typeRef.Generic = &genericType
		} else {
			return TypeRef{}, p.errorf("generic type %s not supported", typeRef.Name)
		}
	}

	// Handle optional types (?)
	if p.cur.Type == TokenQuestion {
		typeRef.Optional = true
		p.nextToken()
	}

	return typeRef, nil
}

// parseConditionalTypeRef parses conditional type references like "flags.0?string"
func (p *Parser) parseConditionalTypeRef() (TypeRef, error) {
	if p.cur.Type != TokenIdent || !strings.HasPrefix(p.cur.Literal, "flags") {
		return TypeRef{}, p.errorf("expected flags identifier, got %s", p.cur.Type)
	}
	flagName := p.cur.Literal
	p.nextToken()      // consume flags identifier
	p.expect(TokenDot) // "."

	if p.cur.Type != TokenNumber {
		return TypeRef{}, p.errorf("expected flag bit number, got %s", p.cur.Type)
	}

	if p.cur.Type != TokenNumber && p.cur.Type != TokenIdent {
		return TypeRef{}, p.errorf("expected flag bit number, got %s", p.cur.Type)
	}
	literal := p.cur.Literal
	p.nextToken()
	if literal == "" || strings.HasPrefix(literal, "0x") || !isAllDigits(literal) {
		return TypeRef{}, p.errorf("expected decimal flag bit number, got %s", literal)
	}
	flagBitValue, err := strconv.Atoi(literal)
	if err != nil {
		return TypeRef{}, p.errorf("invalid flag bit number %s: %v", literal, err)
	}

	p.expect(TokenQuestion) // "?"

	// Parse the actual type
	actualType, err := p.parseTypeRef()
	if err != nil {
		return TypeRef{}, err
	}

	// Mark as optional and store flag bit
	actualType.Optional = true
	actualType.FlagName = flagName
	actualType.FlagBit = &[]int{flagBitValue}[0] // Convert to *int

	return actualType, nil
}

// nextToken advances to the next token.
func (p *Parser) nextToken() {
	p.cur = p.peek
	p.peek = p.lexer.NextToken()
}

// expect checks if current token matches expected type and advances.
func (p *Parser) expect(tokenType TokenType) bool {
	if p.cur.Type == tokenType {
		p.nextToken()
		return true
	}
	return false
}

// skipNewlines skips consecutive newlines.
func (p *Parser) skipNewlines() {
	for p.cur.Type == TokenNewLine {
		p.nextToken()
	}
}

// errorf creates a parse error with current position.
func (p *Parser) errorf(format string, args ...interface{}) *ParseError {
	return NewParseError(p.cur.Line, p.cur.Column, fmt.Sprintf(format, args...), p.cur)
}
