package parser

import (
	"fmt"
	"regexp"
	"strings"
)

// LayerDifference describes the declarations and removals introduced by one
// schema layer.
type LayerDifference struct {
	Layer              int
	Source             string
	Schema             *Schema
	RemoveConstructors []string
	RemoveFunctions    []string
}

var layerDirectivePattern = regexp.MustCompile(
	`^// @tlrpc remove (constructor|function) ([A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*)$`,
)

// ParseLayerDifference parses an ordinary TL fragment and extracts exact
// whole-line TLRPC removal directives from its comments.
func ParseLayerDifference(input string, layer int, source string) (LayerDifference, error) {
	if layer <= 0 {
		return LayerDifference{}, fmt.Errorf("parse layer difference: layer must be positive")
	}
	difference := LayerDifference{
		Layer:  layer,
		Source: source,
	}

	var schemaLines []string
	lines := strings.Split(input, "\n")
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "// @tlrpc") && !layerDirectivePattern.MatchString(trimmed) {
			return LayerDifference{}, fmt.Errorf("parse layer difference %d (%s): invalid directive %q", layer, source, trimmed)
		}
		if match := layerDirectivePattern.FindStringSubmatch(trimmed); match != nil {
			switch match[1] {
			case "constructor":
				difference.RemoveConstructors = append(difference.RemoveConstructors, match[2])
			case "function":
				difference.RemoveFunctions = append(difference.RemoveFunctions, match[2])
			}
		}

		// Comments are not declarations. Removing them here also lets delta
		// fragments contain normal TL comments, which the base parser exposes as
		// lexer tokens rather than silently discarding.
		if comment := strings.Index(line, "//"); comment >= 0 {
			line = line[:comment]
		}
		if strings.TrimSpace(line) != "" {
			schemaLines = append(schemaLines, line)
		}
	}

	schema, err := NewParser(strings.Join(schemaLines, "\n")).ParseWithLayer(layer)
	if err != nil {
		return LayerDifference{}, fmt.Errorf("parse layer difference %d (%s): %w", layer, source, err)
	}
	difference.Schema = schema
	if err := validateLayerDifference(difference); err != nil {
		return LayerDifference{}, err
	}

	return cloneLayerDifference(difference), nil
}

// ResolveLayer applies ordered layer differences through targetLayer. Existing
// declarations are replaced in place, new declarations append, and removals
// close over the state produced by earlier layers.
func ResolveLayer(base *Schema, baseLayer, targetLayer int, differences []LayerDifference) (*Schema, error) {
	if base == nil {
		return nil, fmt.Errorf("resolve layer: base schema is nil")
	}
	if baseLayer <= 0 {
		return nil, fmt.Errorf("resolve layer: base layer must be positive")
	}
	if targetLayer <= 0 {
		return nil, fmt.Errorf("resolve layer: target layer must be positive")
	}
	if base.Layer != baseLayer {
		return nil, fmt.Errorf("resolve layer: base schema layer %d does not match declared base layer %d", base.Layer, baseLayer)
	}
	if targetLayer < baseLayer {
		return nil, fmt.Errorf("resolve layer: target layer %d is below base layer %d", targetLayer, baseLayer)
	}

	targetExists := targetLayer == baseLayer
	previousLayer := baseLayer
	for i, difference := range differences {
		if difference.Schema != nil && difference.Schema.Layer != difference.Layer {
			return nil, fmt.Errorf("%s: schema layer %d does not match difference layer %d", layerDifferenceLabel(difference), difference.Schema.Layer, difference.Layer)
		}
		if difference.Layer <= baseLayer {
			return nil, fmt.Errorf("resolve layer: difference layer %d must be greater than base layer %d", difference.Layer, baseLayer)
		}
		if i > 0 && difference.Layer <= previousLayer {
			return nil, fmt.Errorf("resolve layer: difference layers must be strictly increasing and unique: %d follows %d", difference.Layer, previousLayer)
		}
		previousLayer = difference.Layer
		if difference.Layer == targetLayer {
			targetExists = true
		}
		if err := validateLayerDifference(difference); err != nil {
			return nil, err
		}
	}
	if !targetExists {
		return nil, fmt.Errorf("resolve layer: target layer %d is neither base layer %d nor a supplied difference layer", targetLayer, baseLayer)
	}

	constructors := cloneConstructors(base.Constructors)
	functions := cloneFunctions(base.Functions)
	if err := validateDeclarationSet("base schema", constructors, functions); err != nil {
		return nil, err
	}

	for _, difference := range differences {
		if difference.Layer > targetLayer {
			break
		}

		var err error
		constructors, err = removeConstructors(constructors, difference.RemoveConstructors, difference)
		if err != nil {
			return nil, err
		}
		functions, err = removeFunctions(functions, difference.RemoveFunctions, difference)
		if err != nil {
			return nil, err
		}

		constructors = mergeConstructors(constructors, difference.Schema.Constructors)
		functions = mergeFunctions(functions, difference.Schema.Functions)
		if err := validateDeclarationSet(layerDifferenceLabel(difference), constructors, functions); err != nil {
			return nil, err
		}
	}

	return rebuildSchema(targetLayer, constructors, functions), nil
}

// ResolveSelectedLayer resolves one generation target from a baseline that may
// contain explicitly accepted historical function declarations. Historical
// declarations are excluded while canonical differences are applied, then only
// the declarations accepting targetLayer are added to the selected schema.
func ResolveSelectedLayer(base *Schema, baseLayer, targetLayer int, differences []LayerDifference) (*Schema, error) {
	if base == nil {
		return nil, fmt.Errorf("resolve selected layer: base schema is nil")
	}

	for _, function := range base.Functions {
		if function.VariantLayer != 0 {
			if _, err := ResolveLayers(base, baseLayer, differences); err != nil {
				return nil, err
			}
			break
		}
	}
	canonicalBase := cloneSchema(base)
	canonicalBase.Functions = canonicalBase.Functions[:0]
	for _, function := range base.Functions {
		if function.VariantLayer == 0 {
			canonicalBase.Functions = append(canonicalBase.Functions, cloneFunction(function))
		}
	}
	canonical, err := ResolveLayer(canonicalBase, baseLayer, targetLayer, differences)
	if err != nil {
		return nil, err
	}

	maxLayer := baseLayer
	if len(differences) > 0 {
		maxLayer = differences[len(differences)-1].Layer
	}
	if err := validateHistoricalAcceptance(base, baseLayer, maxLayer, "resolve selected layer"); err != nil {
		return nil, err
	}

	withHistorical := cloneSchema(canonical)
	for _, function := range base.Functions {
		if function.VariantLayer != 0 {
			withHistorical.Functions = append(withHistorical.Functions, cloneFunction(function))
		}
	}
	selected, err := SelectBaselineLayer(withHistorical, targetLayer)
	if err != nil {
		return nil, err
	}
	if err := validateBareLayouts(selected); err != nil {
		return nil, fmt.Errorf("resolve selected layer at layer %d: %w", targetLayer, err)
	}
	return selected, nil
}

func validateLayerDifference(difference LayerDifference) error {
	if difference.Schema == nil {
		return fmt.Errorf("%s: schema is nil", layerDifferenceLabel(difference))
	}
	if err := validateDeclarationSet(layerDifferenceLabel(difference), difference.Schema.Constructors, difference.Schema.Functions); err != nil {
		return err
	}

	constructorNames := make(map[string]struct{}, len(difference.Schema.Constructors)+len(difference.RemoveConstructors))
	for _, constructor := range difference.Schema.Constructors {
		constructorNames[constructor.Name] = struct{}{}
	}
	for _, name := range difference.RemoveConstructors {
		if name == "" {
			return fmt.Errorf("%s: empty constructor removal target", layerDifferenceLabel(difference))
		}
		if _, exists := constructorNames[name]; exists {
			return fmt.Errorf("%s: duplicate constructor name %q inside delta", layerDifferenceLabel(difference), name)
		}
		constructorNames[name] = struct{}{}
	}

	functionNames := make(map[string]struct{}, len(difference.Schema.Functions)+len(difference.RemoveFunctions))
	for _, function := range difference.Schema.Functions {
		functionNames[function.Name] = struct{}{}
	}
	for _, name := range difference.RemoveFunctions {
		if name == "" {
			return fmt.Errorf("%s: empty function removal target", layerDifferenceLabel(difference))
		}
		if _, exists := functionNames[name]; exists {
			return fmt.Errorf("%s: duplicate function name %q inside delta", layerDifferenceLabel(difference), name)
		}
		functionNames[name] = struct{}{}
	}

	return nil
}

func validateDeclarationSet(label string, constructors []Constructor, functions []FuncDecl) error {
	constructorNames := make(map[string]struct{}, len(constructors))
	constructorIDs := make(map[uint32]string, len(constructors))
	for _, constructor := range constructors {
		if _, exists := constructorNames[constructor.Name]; exists {
			return fmt.Errorf("%s: duplicate constructor name %q", label, constructor.Name)
		}
		constructorNames[constructor.Name] = struct{}{}
		if existing, exists := constructorIDs[constructor.ID]; exists && existing != constructor.Name {
			return fmt.Errorf("%s: constructor ID 0x%08x collides between %q and %q", label, constructor.ID, existing, constructor.Name)
		}
		constructorIDs[constructor.ID] = constructor.Name
	}

	functionNames := make(map[string][]FuncDecl, len(functions))
	functionIDs := make(map[uint32]FuncDecl, len(functions))
	for _, function := range functions {
		functionNames[function.Name] = append(functionNames[function.Name], function)
		if existing, exists := functionIDs[function.ID]; exists {
			if existing.Name == function.Name {
				return fmt.Errorf("%s: duplicate function name %q uses ID 0x%08x more than once", label, function.Name, function.ID)
			}
			if !serializerPrefixPair(existing, function) {
				return fmt.Errorf("%s: function ID 0x%08x collides between %q and %q", label, function.ID, existing.Name, function.Name)
			}
		}
		functionIDs[function.ID] = function
	}
	for name, variants := range functionNames {
		if len(variants) == 1 {
			continue
		}
		canonical := 0
		variantLayers := make(map[int]struct{}, len(variants))
		for _, variant := range variants {
			if variant.VariantLayer == 0 {
				canonical++
				continue
			}
			if _, duplicate := variantLayers[variant.VariantLayer]; duplicate {
				return fmt.Errorf("%s: duplicate function name %q has repeated variant layer %d", label, name, variant.VariantLayer)
			}
			variantLayers[variant.VariantLayer] = struct{}{}
		}
		if canonical != 1 {
			return fmt.Errorf("%s: duplicate function name %q requires exactly one canonical declaration and annotated variants", label, name)
		}
	}

	return nil
}

func serializerPrefixPair(first, second FuncDecl) bool {
	return (first.IsHelper && second.IsTemplate && strings.TrimSuffix(first.Name, "Prefix") == second.Name) ||
		(second.IsHelper && first.IsTemplate && strings.TrimSuffix(second.Name, "Prefix") == first.Name)
}

func removeConstructors(current []Constructor, removals []string, difference LayerDifference) ([]Constructor, error) {
	for _, name := range removals {
		index := constructorIndex(current, name)
		if index < 0 {
			return nil, fmt.Errorf("%s: cannot remove missing constructor %q", layerDifferenceLabel(difference), name)
		}
		current = append(current[:index], current[index+1:]...)
	}
	return current, nil
}

func removeFunctions(current []FuncDecl, removals []string, difference LayerDifference) ([]FuncDecl, error) {
	for _, name := range removals {
		index := functionIndex(current, name)
		if index < 0 {
			return nil, fmt.Errorf("%s: cannot remove missing function %q", layerDifferenceLabel(difference), name)
		}
		current = append(current[:index], current[index+1:]...)
	}
	return current, nil
}

func mergeConstructors(current, replacements []Constructor) []Constructor {
	for _, replacement := range replacements {
		replacement = cloneConstructor(replacement)
		if index := constructorIndex(current, replacement.Name); index >= 0 {
			current[index] = replacement
		} else {
			current = append(current, replacement)
		}
	}
	return current
}

func mergeFunctions(current, replacements []FuncDecl) []FuncDecl {
	for _, replacement := range replacements {
		replacement = cloneFunction(replacement)
		if index := functionIndex(current, replacement.Name); index >= 0 {
			current[index] = replacement
		} else {
			current = append(current, replacement)
		}
	}
	return current
}

func constructorIndex(constructors []Constructor, name string) int {
	for i := range constructors {
		if constructors[i].Name == name {
			return i
		}
	}
	return -1
}

func functionIndex(functions []FuncDecl, name string) int {
	for i := range functions {
		if functions[i].Name == name && functions[i].VariantLayer == 0 {
			return i
		}
	}
	return -1
}

func rebuildSchema(layer int, constructors []Constructor, functions []FuncDecl) *Schema {
	schema := NewSchema(layer)
	schema.Constructors = cloneConstructors(constructors)
	schema.Functions = cloneFunctions(functions)

	typeIndexes := make(map[string]int)
	for _, constructor := range schema.Constructors {
		typeName := constructor.ResultType.FullName()
		index, exists := typeIndexes[typeName]
		if !exists {
			index = len(schema.Types)
			typeIndexes[typeName] = index
			schema.Types = append(schema.Types, TypeDecl{Name: typeName})
		}
		schema.Types[index].Constructors = append(schema.Types[index].Constructors, cloneConstructor(constructor))
	}
	for i := range schema.Types {
		schema.Types[i].IsUnion = len(schema.Types[i].Constructors) > 1
		if schema.Types[i].IsUnion {
			schema.UnionTypes[schema.Types[i].Name] = true
		}
	}

	return schema
}

func cloneLayerDifference(difference LayerDifference) LayerDifference {
	return LayerDifference{
		Layer:              difference.Layer,
		Source:             difference.Source,
		Schema:             cloneSchema(difference.Schema),
		RemoveConstructors: append([]string(nil), difference.RemoveConstructors...),
		RemoveFunctions:    append([]string(nil), difference.RemoveFunctions...),
	}
}

func cloneSchema(schema *Schema) *Schema {
	if schema == nil {
		return nil
	}
	cloned := rebuildSchema(schema.Layer, schema.Constructors, schema.Functions)
	cloned.BaseLayer = schema.BaseLayer
	cloned.IsLayered = schema.IsLayered
	return cloned
}

func cloneConstructors(constructors []Constructor) []Constructor {
	cloned := make([]Constructor, len(constructors))
	for i, constructor := range constructors {
		cloned[i] = cloneConstructor(constructor)
	}
	return cloned
}

func cloneConstructor(constructor Constructor) Constructor {
	constructor.GenericParams = append([]GenericParam(nil), constructor.GenericParams...)
	constructor.Params = cloneParameters(constructor.Params)
	constructor.ResultType = cloneTypeRef(constructor.ResultType)
	constructor.VectorCount = cloneStringPointer(constructor.VectorCount)
	constructor.Intervals = cloneIntervals(constructor.Intervals)
	return constructor
}

func cloneFunctions(functions []FuncDecl) []FuncDecl {
	cloned := make([]FuncDecl, len(functions))
	for i, function := range functions {
		cloned[i] = cloneFunction(function)
	}
	return cloned
}

func cloneFunction(function FuncDecl) FuncDecl {
	function.GenericParams = append([]GenericParam(nil), function.GenericParams...)
	function.Params = cloneParameters(function.Params)
	function.ResultType = cloneTypeRef(function.ResultType)
	function.Intervals = cloneIntervals(function.Intervals)
	function.RequestIntervals = cloneIntervals(function.RequestIntervals)
	function.AcceptIntervals = cloneIntervals(function.AcceptIntervals)
	return function
}

func cloneParameters(parameters []Parameter) []Parameter {
	cloned := make([]Parameter, len(parameters))
	for i, parameter := range parameters {
		cloned[i] = parameter
		cloned[i].Type = cloneTypeRef(parameter.Type)
		cloned[i].FlagBit = cloneIntPointer(parameter.FlagBit)
	}
	return cloned
}

func cloneTypeRef(reference TypeRef) TypeRef {
	reference.FlagBit = cloneIntPointer(reference.FlagBit)
	if reference.Generic != nil {
		generic := cloneTypeRef(*reference.Generic)
		reference.Generic = &generic
	}
	return reference
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func layerDifferenceLabel(difference LayerDifference) string {
	if difference.Source == "" {
		return fmt.Sprintf("layer difference %d", difference.Layer)
	}
	return fmt.Sprintf("layer difference %d (%s)", difference.Layer, difference.Source)
}
