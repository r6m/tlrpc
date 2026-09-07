package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var baselineVariantDirectivePattern = regexp.MustCompile(`^// @tlrpc variant-layer ([1-9][0-9]*)$`)
var baselineDeclarationPattern = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)#([0-9A-Fa-f]+)\b`)

type baselineFunctionKey struct {
	name string
	id   uint32
}

// ParseBaselineSchema parses a base schema that may contain explicitly named
// historical forms of a function. Every duplicated function name must have
// exactly one unannotated canonical declaration; each additional declaration
// needs an immediately preceding // @tlrpc variant-layer <N> directive.
func ParseBaselineSchema(input string, layer int, source string) (*Schema, error) {
	annotations := make(map[baselineFunctionKey]int)
	pendingLayer := 0
	inFunctions := false
	lines := strings.Split(input, "\n")
	for i, rawLine := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if match := baselineVariantDirectivePattern.FindStringSubmatch(line); match != nil {
			if pendingLayer != 0 {
				return nil, fmt.Errorf("parse baseline schema %s: variant-layer directive at line %d is not followed by a declaration", source, i)
			}
			value, err := strconv.Atoi(match[1])
			if err != nil {
				return nil, fmt.Errorf("parse baseline schema %s: invalid variant-layer at line %d", source, i+1)
			}
			if layer <= 0 {
				return nil, fmt.Errorf("parse baseline schema %s: variant-layer requires a positive baseline layer", source)
			}
			if value > layer {
				return nil, fmt.Errorf("parse baseline schema %s: variant-layer %d exceeds baseline layer %d", source, value, layer)
			}
			pendingLayer = value
			lines[i] = ""
			continue
		}
		if strings.HasPrefix(line, "// @tlrpc") {
			return nil, fmt.Errorf("parse baseline schema %s: invalid directive %q", source, line)
		}
		if pendingLayer != 0 {
			if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "---") {
				return nil, fmt.Errorf("parse baseline schema %s: variant-layer directive must immediately precede a function declaration", source)
			}
			if !inFunctions {
				return nil, fmt.Errorf("parse baseline schema %s: variant-layer directive applies only to functions", source)
			}
			match := baselineDeclarationPattern.FindStringSubmatch(line)
			if match == nil {
				return nil, fmt.Errorf("parse baseline schema %s: variant-layer directive must immediately precede a function declaration", source)
			}
			parsedID, err := strconv.ParseUint(match[2], 16, 32)
			if err != nil {
				return nil, fmt.Errorf("parse baseline schema %s: invalid function ID %q", source, match[2])
			}
			key := baselineFunctionKey{name: match[1], id: uint32(parsedID)}
			if _, duplicate := annotations[key]; duplicate {
				return nil, fmt.Errorf("parse baseline schema %s: duplicate variant-layer annotation for %s#%08x", source, key.name, key.id)
			}
			annotations[key] = pendingLayer
			pendingLayer = 0
		}
		if strings.HasPrefix(line, "//") {
			lines[i] = ""
			continue
		}
		if comment := strings.Index(lines[i], "//"); comment >= 0 {
			lines[i] = lines[i][:comment]
		}
		switch line {
		case "---functions---":
			inFunctions = true
		case "---types---":
			inFunctions = false
		}
	}
	if pendingLayer != 0 {
		return nil, fmt.Errorf("parse baseline schema %s: variant-layer directive at end of file is not followed by a declaration", source)
	}

	schema, err := NewParser(strings.Join(lines, "\n")).ParseWithLayer(layer)
	if err != nil {
		return nil, err
	}
	for i := range schema.Functions {
		key := baselineFunctionKey{name: schema.Functions[i].Name, id: schema.Functions[i].ID}
		if variantLayer, ok := annotations[key]; ok {
			schema.Functions[i].VariantLayer = variantLayer
			delete(annotations, key)
		}
	}
	if len(annotations) != 0 {
		return nil, fmt.Errorf("parse baseline schema %s: annotated function declaration was not parsed", source)
	}
	if err := validateBaselineFunctionVariants(source, schema.Functions); err != nil {
		return nil, err
	}
	return schema, nil
}

func validateBaselineFunctionVariants(source string, functions []FuncDecl) error {
	byName := make(map[string][]FuncDecl)
	for _, function := range functions {
		byName[function.Name] = append(byName[function.Name], function)
	}
	for name, variants := range byName {
		if len(variants) == 1 {
			if variants[0].VariantLayer != 0 {
				return fmt.Errorf("parse baseline schema %s: function %q has variant-layer %d but no canonical declaration", source, name, variants[0].VariantLayer)
			}
			continue
		}
		canonical := 0
		ids := make(map[uint32]struct{}, len(variants))
		layers := make(map[int]struct{}, len(variants))
		for _, variant := range variants {
			if _, duplicate := ids[variant.ID]; duplicate {
				return fmt.Errorf("parse baseline schema %s: duplicate function %q ID 0x%08x", source, name, variant.ID)
			}
			ids[variant.ID] = struct{}{}
			if variant.VariantLayer == 0 {
				canonical++
				continue
			}
			if _, duplicate := layers[variant.VariantLayer]; duplicate {
				return fmt.Errorf("parse baseline schema %s: duplicate function %q variant-layer %d", source, name, variant.VariantLayer)
			}
			layers[variant.VariantLayer] = struct{}{}
		}
		if canonical != 1 {
			return fmt.Errorf("parse baseline schema %s: duplicate function %q requires exactly one unannotated canonical declaration and annotated historical variants", source, name)
		}
	}
	return nil
}
