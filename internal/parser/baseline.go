package parser

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var baselineVariantDirectivePattern = regexp.MustCompile(`^// @tlrpc variant-layer ([1-9][0-9]*)$`)
var baselineAcceptDirectivePattern = regexp.MustCompile(`^// @tlrpc accept-layers (.+)$`)
var baselineDeclarationPattern = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)#([0-9A-Fa-f]+)\b`)

type baselineAnnotation struct {
	name       string
	id         uint32
	occurrence int
	variant    int
	acceptance []LayerInterval
}

type baselineFunctionKey struct {
	name string
	id   uint32
}

// SelectBaselineLayer returns the declarations accepted at one generation
// target. ParseBaselineSchema intentionally retains all historical metadata so
// ResolveLayers can validate it against a supplied multi-layer history.
func SelectBaselineLayer(schema *Schema, targetLayer int) (*Schema, error) {
	if schema == nil {
		return nil, fmt.Errorf("select baseline layer: schema is nil")
	}
	if targetLayer <= 0 {
		return nil, fmt.Errorf("select baseline layer: target layer must be positive")
	}
	functions := make([]FuncDecl, 0, len(schema.Functions))
	for _, function := range schema.Functions {
		if function.VariantLayer != 0 && !intervalsSupport(function.AcceptIntervals, targetLayer) {
			continue
		}
		selected := cloneFunction(function)
		selected.Intervals = nil
		selected.RequestIntervals = nil
		functions = append(functions, selected)
	}
	selected := rebuildSchema(targetLayer, schema.Constructors, functions)
	if err := validateDeclarationSet("selected baseline schema", selected.Constructors, selected.Functions); err != nil {
		return nil, err
	}
	return selected, nil
}

func intervalsSupport(intervals []LayerInterval, layer int) bool {
	for _, interval := range intervals {
		if layer >= interval.MinLayer && (interval.MaxLayer == 0 || layer <= interval.MaxLayer) {
			return true
		}
	}
	return false
}

// ParseBaselineSchema parses a base schema with explicitly named historical
// function forms. Directives bind to a declaration occurrence, so same-ID
// historical contracts do not alias one another.
func ParseBaselineSchema(input string, layer int, source string) (*Schema, error) {
	var annotations []baselineAnnotation
	pendingVariant := 0
	var pendingAcceptance []LayerInterval
	inFunctions := false
	declarationOccurrences := make(map[baselineFunctionKey]int)
	lines := strings.Split(input, "\n")
	for i, rawLine := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if match := baselineVariantDirectivePattern.FindStringSubmatch(line); match != nil {
			if pendingVariant != 0 {
				return nil, fmt.Errorf("parse baseline schema %s: duplicate variant-layer directive at line %d", source, i+1)
			}
			value, err := strconv.Atoi(match[1])
			if err != nil || layer <= 0 || value > layer {
				return nil, fmt.Errorf("parse baseline schema %s: invalid variant-layer at line %d", source, i+1)
			}
			pendingVariant = value
			lines[i] = ""
			continue
		}
		if match := baselineAcceptDirectivePattern.FindStringSubmatch(line); match != nil {
			if pendingAcceptance != nil {
				return nil, fmt.Errorf("parse baseline schema %s: duplicate accept-layers directive at line %d", source, i+1)
			}
			intervals, err := parseLayerIntervals(match[1])
			if err != nil {
				return nil, fmt.Errorf("parse baseline schema %s: accept-layers at line %d: %w", source, i+1, err)
			}
			pendingAcceptance = intervals
			lines[i] = ""
			continue
		}
		if strings.HasPrefix(line, "// @tlrpc") {
			return nil, fmt.Errorf("parse baseline schema %s: invalid directive %q", source, line)
		}
		if pendingVariant != 0 || pendingAcceptance != nil {
			if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "---") || !inFunctions {
				return nil, fmt.Errorf("parse baseline schema %s: directives must be adjacent to and immediately precede a function declaration", source)
			}
			match := baselineDeclarationPattern.FindStringSubmatch(line)
			if match == nil {
				return nil, fmt.Errorf("parse baseline schema %s: directives must immediately precede a function declaration", source)
			}
			parsedID, err := strconv.ParseUint(match[2], 16, 32)
			if err != nil {
				return nil, fmt.Errorf("parse baseline schema %s: invalid function ID %q", source, match[2])
			}
			key := baselineFunctionKey{name: match[1], id: uint32(parsedID)}
			annotations = append(annotations, baselineAnnotation{name: key.name, id: key.id, occurrence: declarationOccurrences[key], variant: pendingVariant, acceptance: pendingAcceptance})
			pendingVariant = 0
			pendingAcceptance = nil
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
		if inFunctions {
			if match := baselineDeclarationPattern.FindStringSubmatch(line); match != nil {
				parsedID, err := strconv.ParseUint(match[2], 16, 32)
				if err == nil {
					declarationOccurrences[baselineFunctionKey{name: match[1], id: uint32(parsedID)}]++
				}
			}
		}
	}
	if pendingVariant != 0 || pendingAcceptance != nil {
		return nil, fmt.Errorf("parse baseline schema %s: directive at end of file is not followed by a declaration", source)
	}

	schema, err := NewParser(strings.Join(lines, "\n")).ParseWithLayer(layer)
	if err != nil {
		return nil, err
	}
	for _, annotation := range annotations {
		occurrence := 0
		var function *FuncDecl
		for i := range schema.Functions {
			candidate := &schema.Functions[i]
			if candidate.Name != annotation.name || candidate.ID != annotation.id {
				continue
			}
			if occurrence == annotation.occurrence {
				function = candidate
				break
			}
			occurrence++
		}
		if function == nil {
			return nil, fmt.Errorf("parse baseline schema %s: annotated function declaration occurrence was not parsed", source)
		}
		function.VariantLayer = annotation.variant
		function.RequestVariantLayer = annotation.variant
		function.AcceptIntervals = cloneIntervals(annotation.acceptance)
	}
	if err := validateBaselineFunctionVariants(source, schema.Functions); err != nil {
		return nil, err
	}
	return schema, nil
}

func parseLayerIntervals(value string) ([]LayerInterval, error) {
	parts := strings.Split(value, ",")
	intervals := make([]LayerInterval, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		bounds := strings.Split(part, "-")
		if len(bounds) != 2 {
			return nil, fmt.Errorf("expected bounded MIN-MAX interval, got %q", part)
		}
		minLayer, err := strconv.Atoi(bounds[0])
		if err != nil || minLayer <= 0 {
			return nil, fmt.Errorf("invalid minimum layer in %q", part)
		}
		maxLayer, err := strconv.Atoi(bounds[1])
		if err != nil || maxLayer < minLayer {
			return nil, fmt.Errorf("invalid maximum layer in %q", part)
		}
		intervals = append(intervals, LayerInterval{MinLayer: minLayer, MaxLayer: maxLayer})
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].MinLayer < intervals[j].MinLayer })
	for i := 1; i < len(intervals); i++ {
		if intervals[i].MinLayer <= intervals[i-1].MaxLayer {
			return nil, fmt.Errorf("overlapping or duplicate intervals")
		}
		if intervals[i].MinLayer == intervals[i-1].MaxLayer+1 {
			intervals[i-1].MaxLayer = intervals[i].MaxLayer
			intervals = append(intervals[:i], intervals[i+1:]...)
			i--
		}
	}
	return intervals, nil
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
		layers := make(map[int]struct{}, len(variants))
		for _, variant := range variants {
			if variant.VariantLayer == 0 {
				canonical++
				continue
			}
			if len(variant.AcceptIntervals) == 0 {
				return fmt.Errorf("parse baseline schema %s: historical function %q layer %d requires accept-layers", source, name, variant.VariantLayer)
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

func cloneIntervals(intervals []LayerInterval) []LayerInterval {
	return append([]LayerInterval(nil), intervals...)
}
