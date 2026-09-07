package parser

import (
	"fmt"
	"reflect"
	"sort"
)

// LayeredSchema is the generation view of a base schema and its ordered layer
// differences. Schema contains every historical wire shape needed by one Go
// package. Unchanged declarations occur once; changed declarations carry a
// VariantLayer suffix and an inclusive validity range.
type LayeredSchema struct {
	BaseLayer int
	MaxLayer  int
	Layers    []int
	Schema    *Schema
}

// ResolveLayers builds one deterministic generation schema containing the
// base layer and every supplied difference. It preserves declarations removed
// from later layers so historical incoming constructors remain decodable.
func ResolveLayers(base *Schema, baseLayer int, differences []LayerDifference) (*LayeredSchema, error) {
	if base == nil {
		return nil, fmt.Errorf("resolve layers: base schema is nil")
	}
	maxLayer := baseLayer
	if len(differences) > 0 {
		maxLayer = differences[len(differences)-1].Layer
	}
	// ResolveLayer owns sequence, removal, and per-layer collision validation.
	if _, err := ResolveLayer(base, baseLayer, maxLayer, differences); err != nil {
		return nil, err
	}

	layers := make([]int, 0, len(differences)+1)
	snapshots := make([]*Schema, 0, len(differences)+1)
	layers = append(layers, baseLayer)
	baseSnapshot, err := ResolveLayer(base, baseLayer, baseLayer, differences)
	if err != nil {
		return nil, err
	}
	snapshots = append(snapshots, baseSnapshot)
	for _, difference := range differences {
		resolved, err := ResolveLayer(base, baseLayer, difference.Layer, differences)
		if err != nil {
			return nil, err
		}
		layers = append(layers, difference.Layer)
		snapshots = append(snapshots, resolved)
	}

	combined := NewSchema(maxLayer)
	combined.BaseLayer = baseLayer
	combined.IsLayered = true
	stableUnions := stableUnionNames(snapshots)
	typeVersions := make(map[string]int)
	constructorVersions := make(map[string]int)
	functionVersions := make(map[string]int)
	seenTypes := make(map[string]bool)
	seenConstructors := make(map[string]bool)
	seenFunctions := make(map[string]bool)
	typeIndexes := make(map[string]int)
	constructorIndexes := make(map[string]int)
	functionIndexes := make(map[string]int)
	activeConstructors := make(map[string]int)
	activeFunctions := make(map[string]int)

	var previous *Schema
	for snapshotIndex, snapshot := range snapshots {
		layer := layers[snapshotIndex]
		changedTypes := changedTypeNames(previous, snapshot, seenTypes, stableUnions)
		propagateReferencedTypeChanges(changedTypes, stableUnions, previous, snapshot)

		previousTypes := typeDeclsByName(previous)
		for _, decl := range snapshot.Types {
			if !stableUnions[decl.Name] && seenTypes[decl.Name] && (changedTypes[decl.Name] || previousTypes[decl.Name] == nil) {
				typeVersions[decl.Name] = layer
			}
			seenTypes[decl.Name] = true
		}

		previousConstructors := constructorsByName(previous)
		currentConstructorKeys := make(map[string]struct{})
		for _, constructor := range snapshot.Constructors {
			old := previousConstructors[constructor.Name]
			additive := old != nil && additiveFlagExtension(*old, constructor)
			changed := old != nil && !reflect.DeepEqual(*old, constructor) && !additive
			changed = changed || referencesChangedType(constructor.Params, changedTypes)
			if seenConstructors[constructor.Name] && (changed || old == nil) {
				constructorVersions[constructor.Name] = layer
			}
			seenConstructors[constructor.Name] = true

			projected := cloneConstructor(constructor)
			projected.VariantLayer = constructorVersions[constructor.Name]
			projected.MinLayer = layer
			projected.MaxLayer = 0
			projected.OutputMinLayer = layer
			projected.OutputMaxLayer = 0
			projected.Params = annotateParameters(projected.Params, typeVersions)
			projected.ResultType = annotateTypeRef(projected.ResultType, typeVersions)
			key := variantKey(projected.Name, projected.VariantLayer)
			if additive {
				if index, exists := constructorIndexes[key]; exists {
					projected = mergeAdditiveConstructor(combined.Constructors[index], projected, layer)
				}
			}
			currentConstructorKeys[key] = struct{}{}
			if index, exists := constructorIndexes[key]; exists {
				if additive {
					combined.Constructors[index] = projected
				} else {
					combined.Constructors[index].MaxLayer = 0
					combined.Constructors[index].OutputMaxLayer = 0
				}
			} else {
				constructorIndexes[key] = len(combined.Constructors)
				combined.Constructors = append(combined.Constructors, projected)
			}
			activeConstructors[key] = constructorIndexes[key]
		}
		closeInactiveConstructors(combined, activeConstructors, currentConstructorKeys, layer-1)

		previousFunctions := functionsByName(previous)
		currentFunctionKeys := make(map[string]struct{})
		for _, function := range snapshot.Functions {
			if function.VariantLayer != 0 {
				projected := cloneFunction(function)
				projected.MinLayer = baseLayer
				projected.MaxLayer = 0
				projected.Params = annotateParameters(projected.Params, typeVersions)
				projected.ResultType = annotateTypeRef(projected.ResultType, typeVersions)
				key := variantKey(projected.Name, projected.VariantLayer)
				currentFunctionKeys[key] = struct{}{}
				if index, exists := functionIndexes[key]; exists {
					combined.Functions[index] = projected
				} else {
					functionIndexes[key] = len(combined.Functions)
					combined.Functions = append(combined.Functions, projected)
				}
				activeFunctions[key] = functionIndexes[key]
				continue
			}
			old := previousFunctions[function.Name]
			changed := old != nil && !sameFunctionRequest(*old, function)
			changed = changed || referencesChangedType(function.Params, changedTypes)
			if seenFunctions[function.Name] && (changed || old == nil) {
				functionVersions[function.Name] = layer
			}
			seenFunctions[function.Name] = true

			projected := cloneFunction(function)
			projected.VariantLayer = functionVersions[function.Name]
			projected.MinLayer = layer
			projected.MaxLayer = 0
			projected.Params = annotateParameters(projected.Params, typeVersions)
			projected.ResultType = annotateTypeRef(projected.ResultType, typeVersions)
			key := variantKey(projected.Name, projected.VariantLayer)
			currentFunctionKeys[key] = struct{}{}
			if index, exists := functionIndexes[key]; exists {
				combined.Functions[index].MaxLayer = 0
			} else {
				functionIndexes[key] = len(combined.Functions)
				combined.Functions = append(combined.Functions, projected)
			}
			activeFunctions[key] = functionIndexes[key]
		}
		closeInactiveFunctions(combined, activeFunctions, currentFunctionKeys, layer-1)

		for _, decl := range snapshot.Types {
			projected := TypeDecl{
				Name:         decl.Name,
				IsUnion:      stableUnions[decl.Name] || decl.IsUnion,
				VariantLayer: typeVersions[decl.Name],
			}
			for _, constructor := range decl.Constructors {
				key := variantKey(constructor.Name, constructorVersions[constructor.Name])
				index, ok := constructorIndexes[key]
				if !ok {
					return nil, fmt.Errorf("resolve layers: missing constructor variant %s", key)
				}
				projected.Constructors = append(projected.Constructors, cloneConstructor(combined.Constructors[index]))
			}
			key := variantKey(projected.Name, projected.VariantLayer)
			if index, exists := typeIndexes[key]; exists {
				combined.Types[index] = mergeHistoricalType(combined.Types[index], projected)
			} else {
				typeIndexes[key] = len(combined.Types)
				combined.Types = append(combined.Types, projected)
				if projected.IsUnion {
					combined.UnionTypes[key] = true
				}
			}
		}
		previous = snapshot
	}

	normalizeUniqueIDRanges(combined.Constructors, combined.Functions)
	syncTypeConstructors(combined)
	return &LayeredSchema{BaseLayer: baseLayer, MaxLayer: maxLayer, Layers: layers, Schema: combined}, nil
}

func syncTypeConstructors(schema *Schema) {
	constructors := make(map[string]Constructor, len(schema.Constructors))
	for _, constructor := range schema.Constructors {
		constructors[variantKey(constructor.Name, constructor.VariantLayer)] = constructor
	}
	for i := range schema.Types {
		for j := range schema.Types[i].Constructors {
			constructor := schema.Types[i].Constructors[j]
			if canonical, ok := constructors[variantKey(constructor.Name, constructor.VariantLayer)]; ok {
				schema.Types[i].Constructors[j] = cloneConstructor(canonical)
			}
		}
	}
}

func sameFunctionRequest(old, next FuncDecl) bool {
	return old.Name == next.Name && old.ID == next.ID &&
		reflect.DeepEqual(old.GenericParams, next.GenericParams) &&
		reflect.DeepEqual(old.Params, next.Params) &&
		old.IsTemplate == next.IsTemplate && old.IsHelper == next.IsHelper
}

func stableUnionNames(snapshots []*Schema) map[string]bool {
	unions := make(map[string]bool)
	for _, snapshot := range snapshots {
		if snapshot == nil {
			continue
		}
		for _, declaration := range snapshot.Types {
			if declaration.IsUnion {
				unions[declaration.Name] = true
			}
		}
	}
	return unions
}

func changedTypeNames(previous, current *Schema, seen map[string]bool, stableUnions map[string]bool) map[string]bool {
	changed := make(map[string]bool)
	if previous == nil {
		return changed
	}
	oldTypes := typeDeclsByName(previous)
	newTypes := typeDeclsByName(current)
	for name, old := range oldTypes {
		if stableUnions[name] {
			continue
		}
		if next := newTypes[name]; next != nil && !compatibleTypeEvolution(*old, *next) {
			changed[name] = true
		}
	}
	for name := range newTypes {
		if stableUnions[name] {
			continue
		}
		if oldTypes[name] == nil && seen[name] {
			changed[name] = true
		}
	}
	return changed
}

func compatibleTypeEvolution(old, next TypeDecl) bool {
	oldConstructors := make(map[string]Constructor, len(old.Constructors))
	for _, constructor := range old.Constructors {
		oldConstructors[constructor.Name] = constructor
	}
	for _, constructor := range next.Constructors {
		previous, exists := oldConstructors[constructor.Name]
		if !exists {
			continue // Stable unions retain constructors introduced or removed by a layer.
		}
		if reflect.DeepEqual(previous, constructor) || additiveFlagExtension(previous, constructor) {
			continue
		}
		return false
	}
	return true
}

func additiveFlagExtension(old, next Constructor) bool {
	if old.Name != next.Name || old.ID != next.ID || !reflect.DeepEqual(old.ResultType, next.ResultType) || len(next.Params) <= len(old.Params) {
		return false
	}
	oldByName := make(map[string]Parameter, len(old.Params))
	for _, parameter := range old.Params {
		oldByName[parameter.Name] = parameter
	}
	for _, parameter := range next.Params {
		previous, exists := oldByName[parameter.Name]
		if exists {
			if !reflect.DeepEqual(previous, parameter) {
				return false
			}
			continue
		}
		if parameter.FlagBit == nil && parameter.Type.FlagBit == nil && !(parameter.Type.Name == "#") {
			return false
		}
	}
	return true
}

func mergeAdditiveConstructor(old, next Constructor, layer int) Constructor {
	ranges := make(map[string]Parameter, len(old.Params))
	for _, parameter := range old.Params {
		ranges[parameter.Name] = parameter
	}
	for i := range next.Params {
		if previous, exists := ranges[next.Params[i].Name]; exists {
			next.Params[i].MinLayer = previous.MinLayer
			next.Params[i].MaxLayer = previous.MaxLayer
			continue
		}
		next.Params[i].MinLayer = layer
	}
	next.MinLayer = old.MinLayer
	next.MaxLayer = old.MaxLayer
	next.OutputMinLayer = old.OutputMinLayer
	next.OutputMaxLayer = old.OutputMaxLayer
	return next
}

func mergeHistoricalType(old, next TypeDecl) TypeDecl {
	seen := make(map[string]struct{}, len(old.Constructors))
	for _, constructor := range old.Constructors {
		seen[variantKey(constructor.Name, constructor.VariantLayer)] = struct{}{}
	}
	for _, constructor := range next.Constructors {
		key := variantKey(constructor.Name, constructor.VariantLayer)
		if _, exists := seen[key]; exists {
			for i := range old.Constructors {
				if variantKey(old.Constructors[i].Name, old.Constructors[i].VariantLayer) == key {
					old.Constructors[i] = constructor
					break
				}
			}
			continue
		}
		old.Constructors = append(old.Constructors, constructor)
		seen[key] = struct{}{}
	}
	old.IsUnion = len(old.Constructors) > 1
	return old
}

func propagateReferencedTypeChanges(changed map[string]bool, stableUnions map[string]bool, schemas ...*Schema) {
	reverse := make(map[string]map[string]struct{})
	for _, schema := range schemas {
		if schema == nil {
			continue
		}
		for _, decl := range schema.Types {
			for _, constructor := range decl.Constructors {
				for _, referenced := range referencedTypes(constructor.Params) {
					if reverse[referenced] == nil {
						reverse[referenced] = make(map[string]struct{})
					}
					reverse[referenced][decl.Name] = struct{}{}
				}
			}
		}
	}
	queue := make([]string, 0, len(changed))
	for name := range changed {
		queue = append(queue, name)
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		for owner := range reverse[name] {
			if stableUnions[owner] || changed[owner] {
				continue
			}
			changed[owner] = true
			queue = append(queue, owner)
		}
	}
}

func typeDeclsByName(schema *Schema) map[string]*TypeDecl {
	out := make(map[string]*TypeDecl)
	if schema == nil {
		return out
	}
	for i := range schema.Types {
		out[schema.Types[i].Name] = &schema.Types[i]
	}
	return out
}

func constructorsByName(schema *Schema) map[string]*Constructor {
	out := make(map[string]*Constructor)
	if schema == nil {
		return out
	}
	for i := range schema.Constructors {
		out[schema.Constructors[i].Name] = &schema.Constructors[i]
	}
	return out
}

func functionsByName(schema *Schema) map[string]*FuncDecl {
	out := make(map[string]*FuncDecl)
	if schema == nil {
		return out
	}
	for i := range schema.Functions {
		if schema.Functions[i].VariantLayer == 0 {
			out[schema.Functions[i].Name] = &schema.Functions[i]
		}
	}
	return out
}

func referencedTypes(parameters []Parameter) []string {
	seen := make(map[string]struct{})
	for _, parameter := range parameters {
		collectTypeRefs(parameter.Type, seen)
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func collectTypeRefs(reference TypeRef, out map[string]struct{}) {
	if reference.IsVector && reference.Generic != nil {
		collectTypeRefs(*reference.Generic, out)
		return
	}
	if !reference.IsBuiltin() && !reference.IsTypeVar {
		out[reference.FullName()] = struct{}{}
	}
}

func referencesChangedType(parameters []Parameter, changed map[string]bool) bool {
	for _, name := range referencedTypes(parameters) {
		if changed[name] {
			return true
		}
	}
	return false
}

func typeRefChanged(reference TypeRef, changed map[string]bool) bool {
	seen := make(map[string]struct{})
	collectTypeRefs(reference, seen)
	for name := range seen {
		if changed[name] {
			return true
		}
	}
	return false
}

func annotateParameters(parameters []Parameter, versions map[string]int) []Parameter {
	parameters = cloneParameters(parameters)
	for i := range parameters {
		parameters[i].Type = annotateTypeRef(parameters[i].Type, versions)
	}
	return parameters
}

func annotateTypeRef(reference TypeRef, versions map[string]int) TypeRef {
	reference = cloneTypeRef(reference)
	if reference.Generic != nil {
		generic := annotateTypeRef(*reference.Generic, versions)
		reference.Generic = &generic
	}
	if !reference.IsBuiltin() && !reference.IsTypeVar {
		reference.VariantLayer = versions[reference.FullName()]
	}
	return reference
}

func variantKey(name string, layer int) string {
	return fmt.Sprintf("%s@%d", name, layer)
}

func closeInactiveConstructors(schema *Schema, active map[string]int, current map[string]struct{}, maxLayer int) {
	for key, index := range active {
		if _, ok := current[key]; ok {
			continue
		}
		schema.Constructors[index].MaxLayer = maxLayer
		schema.Constructors[index].OutputMaxLayer = maxLayer
		delete(active, key)
	}
}

func closeInactiveFunctions(schema *Schema, active map[string]int, current map[string]struct{}, maxLayer int) {
	for key, index := range active {
		if _, ok := current[key]; ok {
			continue
		}
		schema.Functions[index].MaxLayer = maxLayer
		delete(active, key)
	}
}

func normalizeUniqueIDRanges(constructors []Constructor, functions []FuncDecl) {
	constructorCounts := make(map[uint32]int)
	for _, constructor := range constructors {
		constructorCounts[constructor.ID]++
	}
	for i := range constructors {
		if constructorCounts[constructors[i].ID] == 1 {
			constructors[i].MaxLayer = 0
		}
	}
	functionCounts := make(map[uint32]int)
	for _, function := range functions {
		if !function.IsHelper {
			functionCounts[function.ID]++
		}
	}
	for i := range functions {
		if !functions[i].IsHelper && functionCounts[functions[i].ID] == 1 {
			functions[i].MaxLayer = 0
		}
	}
}
