// Package codegen provides TL schema parsing.
package codegen

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Parser parses TL schema files.
type Parser struct{}

// NewParser creates a new parser.
func NewParser() *Parser {
	return &Parser{}
}

// ParseFile parses a TL schema file.
func (p *Parser) ParseFile(filename string) (*Schema, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	schema := &Schema{
		Types:    make([]*Type, 0),
		Functions: make([]*Function, 0),
	}

	scanner := bufio.NewScanner(file)
	currentSection := ""

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		// Check for section headers
		if line == "---types---" {
			currentSection = "types"
			continue
		}
		if line == "---functions---" {
			currentSection = "functions"
			continue
		}

		// Parse based on section
		switch currentSection {
		case "types":
			if typ, err := p.parseType(line); err == nil {
				schema.Types = append(schema.Types, typ)
			}
		case "functions":
			if fn, err := p.parseFunction(line); err == nil {
				schema.Functions = append(schema.Functions, fn)
			}
		}
	}

	return schema, scanner.Err()
}

// parseType parses a type definition.
func (p *Parser) parseType(line string) (*Type, error) {
	// Simple regex for type parsing
	re := regexp.MustCompile(`^(\w+)#([0-9a-f]+)\s+(.+)\s*=\s*(\w+);?$`)
	matches := re.FindStringSubmatch(line)
	if len(matches) != 5 {
		return nil, fmt.Errorf("invalid type definition: %s", line)
	}

	constructorID, err := strconv.ParseUint(matches[2], 16, 32)
	if err != nil {
		return nil, err
	}

	return &Type{
		Name:          matches[1],
		ConstructorID: uint32(constructorID),
		Params:        p.parseParams(matches[3]),
		ResultType:    matches[4],
	}, nil
}

// parseFunction parses a function definition.
func (p *Parser) parseFunction(line string) (*Function, error) {
	// Similar to type parsing
	re := regexp.MustCompile(`^(\w+)#([0-9a-f]+)\s+(.+)\s*=\s*(.+);?$`)
	matches := re.FindStringSubmatch(line)
	if len(matches) != 5 {
		return nil, fmt.Errorf("invalid function definition: %s", line)
	}

	constructorID, err := strconv.ParseUint(matches[2], 16, 32)
	if err != nil {
		return nil, err
	}

	return &Function{
		Name:          matches[1],
		ConstructorID: uint32(constructorID),
		Params:        p.parseParams(matches[3]),
		ResultType:    matches[4],
	}, nil
}

// parseParams parses parameter definitions.
func (p *Parser) parseParams(paramsStr string) []*Param {
	if paramsStr == "" {
		return nil
	}

	params := make([]*Param, 0)
	parts := strings.Fields(paramsStr)

	for i := 0; i < len(parts); i += 2 {
		if i+1 >= len(parts) {
			break
		}

		param := &Param{
			Name: parts[i],
			Type: parts[i+1],
		}
		params = append(params, param)
	}

	return params
}