package parser

import (
	"encoding/json"
	"fmt"
	"sort"
)

// LayeredSchema is the generation view of a base schema and its ordered layer
// differences. Distinct contracts occur once and carry their actual interval
// sets, including gaps caused by removal and reappearance.
type LayeredSchema struct {
	BaseLayer int
	MaxLayer  int
	Layers    []int
	Schema    *Schema
}

// ResolveLayers builds one deterministic generation schema. Boxed references
// retain stable family identity; only bare dependencies propagate concrete
// layout changes.
func ResolveLayers(base *Schema, baseLayer int, differences []LayerDifference) (*LayeredSchema, error) {
	if base == nil {
		return nil, fmt.Errorf("resolve layers: base schema is nil")
	}
	maxLayer := baseLayer
	if len(differences) > 0 {
		maxLayer = differences[len(differences)-1].Layer
	}
	if err := validateHistoricalAcceptance(base, baseLayer, maxLayer, "resolve layers"); err != nil {
		return nil, err
	}
	snapshotBase := cloneSchema(base)
	snapshotBase.Functions = snapshotBase.Functions[:0]
	for _, function := range base.Functions {
		if function.VariantLayer == 0 {
			snapshotBase.Functions = append(snapshotBase.Functions, cloneFunction(function))
		}
	}
	if _, err := ResolveLayer(snapshotBase, baseLayer, maxLayer, differences); err != nil {
		return nil, err
	}

	layers := []int{baseLayer}
	snapshots := make([]*Schema, 0, len(differences)+1)
	for _, layer := range append([]int{baseLayer}, differenceLayers(differences)...) {
		snapshot, err := ResolveLayer(snapshotBase, baseLayer, layer, differences)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
		if err := validateBareLayouts(snapshot); err != nil {
			return nil, fmt.Errorf("resolve layers at layer %d: %w", layer, err)
		}
		if layer != baseLayer {
			layers = append(layers, layer)
		}
	}

	if err := validateBareConstructorNames(snapshots, base.Functions); err != nil {
		return nil, err
	}
	combined := NewSchema(maxLayer)
	combined.BaseLayer = baseLayer
	combined.IsLayered = true

	constructorVariants := make(map[string]map[string]int)
	constructorIndexes := make(map[string]int)
	requestVariants := make(map[string]map[string]int)
	handlerVariants := make(map[string]map[string]int)
	typeVariants := make(map[string]map[string]int)
	functionIndexes := make(map[string]int)
	typeVersions := make(map[string]int)
	versionsBySnapshot := make([]map[string]int, 0, len(snapshots))
	activeConstructors := make(map[string]int)
	activeFunctions := make(map[string]int)
	families := make(map[string]int)

	for snapshotIndex, snapshot := range snapshots {
		layer := layers[snapshotIndex]
		typeVersions = make(map[string]int)
		for _, declaration := range snapshot.Types {
			signature, err := resolvedTypeContractSignature(declaration.Name, snapshot, nil)
			if err != nil {
				return nil, fmt.Errorf("resolve layers at layer %d: %w", layer, err)
			}
			typeVersions[declaration.Name] = contractVariantLayer(typeVariants, declaration.Name, signature, layer)
		}

		versionsBySnapshot = append(versionsBySnapshot, typeVersions)
		currentConstructors := make(map[string]struct{})
		for _, constructor := range snapshot.Constructors {
			projected := cloneConstructor(constructor)
			projected.Params = annotateBareParameters(projected.Params, typeVersions)
			projected.ResultType = annotateBareTypeRef(projected.ResultType, typeVersions)
			signature := constructorContractSignature(projected)
			variants := constructorVariants[projected.Name]
			if variants == nil {
				variants = make(map[string]int)
				constructorVariants[projected.Name] = variants
			}
			variantLayer, exists := variants[signature]
			if !exists {
				if len(variants) != 0 {
					variantLayer = layer
				}
				variants[signature] = variantLayer
			}
			projected.VariantLayer = variantLayer
			key := variantKey(projected.Name, variantLayer)
			currentConstructors[key] = struct{}{}
			if index, ok := constructorIndexes[key]; ok {
				openInterval(&combined.Constructors[index].Intervals, layer)
				activeConstructors[key] = index
				continue
			}
			projected.Intervals = []LayerInterval{{MinLayer: layer}}
			constructorIndexes[key] = len(combined.Constructors)
			activeConstructors[key] = len(combined.Constructors)
			combined.Constructors = append(combined.Constructors, projected)
		}
		closeInactiveConstructorIntervals(combined, activeConstructors, currentConstructors, layer-1)

		currentFunctions := make(map[string]struct{})
		for _, function := range snapshot.Functions {
			if function.VariantLayer != 0 {
				continue
			}
			projected := cloneFunction(function)
			projected.Params = annotateBareParameters(projected.Params, typeVersions)
			projected.ResultType = annotateBareTypeRef(projected.ResultType, typeVersions)
			requestSignature := functionRequestSignature(projected)
			projected.RequestVariantLayer = contractVariantLayer(requestVariants, projected.Name, requestSignature, layer)
			handlerSignature := semanticKey(struct {
				RequestVariantLayer int
				Result              semanticTypeRef
			}{projected.RequestVariantLayer, contractTypeRef(projected.ResultType)})
			projected.VariantLayer = contractVariantLayer(handlerVariants, projected.Name, handlerSignature, layer)
			key := variantKey(projected.Name, projected.VariantLayer)
			currentFunctions[key] = struct{}{}
			if index, ok := functionIndexes[key]; ok {
				openInterval(&combined.Functions[index].Intervals, layer)
				activeFunctions[key] = index
				continue
			}
			projected.Intervals = []LayerInterval{{MinLayer: layer}}
			functionIndexes[key] = len(combined.Functions)
			activeFunctions[key] = len(combined.Functions)
			combined.Functions = append(combined.Functions, projected)
		}
		closeInactiveFunctionIntervals(combined, activeFunctions, currentFunctions, layer-1)
	}

	// Historical declarations are policy exceptions, not active snapshot state.
	for _, function := range base.Functions {
		if function.VariantLayer == 0 {
			continue
		}
		projected := cloneFunction(function)
		projected.RequestVariantLayer = function.RequestVariantLayer
		if projected.RequestVariantLayer == 0 {
			projected.RequestVariantLayer = projected.VariantLayer
		}
		projected.Intervals = intersectIntervals(projected.AcceptIntervals, baseLayer, maxLayer)
		if len(projected.Intervals) == 0 {
			continue
		}
		var selectedSignature string
		for i, snapshot := range snapshots {
			lastLayer := maxLayer
			if i+1 < len(layers) {
				lastLayer = layers[i+1] - 1
			}
			if len(intersectIntervals(projected.Intervals, layers[i], lastLayer)) == 0 {
				continue
			}
			candidate := cloneFunction(function)
			for _, parameter := range candidate.Params {
				for _, family := range bareReferenceNames(parameter.Type) {
					if _, ok := snapshot.FindType(family); !ok {
						return nil, fmt.Errorf("historical method %s: bare family %s is unavailable at layer %d", function.Name, family, layers[i])
					}
				}
			}
			candidate.Params = annotateBareParameters(candidate.Params, versionsBySnapshot[i])
			candidate.ResultType = annotateBareTypeRef(candidate.ResultType, versionsBySnapshot[i])
			signature := functionRequestSignature(candidate) + semanticKey(contractTypeRef(candidate.ResultType))
			if selectedSignature != "" && selectedSignature != signature {
				return nil, fmt.Errorf("historical method %s: bare contract changes within accepted layers", function.Name)
			}
			selectedSignature = signature
			projected.Params, projected.ResultType = candidate.Params, candidate.ResultType
		}
		key := variantKey(projected.Name, projected.VariantLayer)
		if _, duplicate := functionIndexes[key]; duplicate {
			return nil, fmt.Errorf("resolve layers: duplicate historical handler contract %s", key)
		}
		functionIndexes[key] = len(combined.Functions)
		combined.Functions = append(combined.Functions, projected)
	}
	requestIntervals := make(map[string][]LayerInterval)
	for _, function := range combined.Functions {
		key := variantKey(function.Name, function.RequestVariantLayer)
		requestIntervals[key] = append(requestIntervals[key], function.Intervals...)
	}
	for i := range combined.Functions {
		key := variantKey(combined.Functions[i].Name, combined.Functions[i].RequestVariantLayer)
		combined.Functions[i].RequestIntervals = normalizeIntervals(requestIntervals[key])
	}

	for _, constructor := range combined.Constructors {
		family := constructor.ResultType.FullName()
		index, exists := families[family]
		if !exists {
			index = len(combined.Types)
			families[family] = index
			combined.Types = append(combined.Types, TypeDecl{Name: family})
		}
		combined.Types[index].Constructors = append(combined.Types[index].Constructors, cloneConstructor(constructor))
	}
	for i := range combined.Types {
		combined.Types[i].IsUnion = len(combined.Types[i].Constructors) > 1
		if combined.Types[i].IsUnion {
			combined.UnionTypes[combined.Types[i].Name] = true
		}
		sort.SliceStable(combined.Types[i].Constructors, func(a, b int) bool {
			left, right := combined.Types[i].Constructors[a], combined.Types[i].Constructors[b]
			if left.Name == right.Name {
				return left.VariantLayer < right.VariantLayer
			}
			return left.Name < right.Name
		})
	}
	if err := validateLayeredIntervals(combined); err != nil {
		return nil, err
	}
	return &LayeredSchema{BaseLayer: baseLayer, MaxLayer: maxLayer, Layers: layers, Schema: combined}, nil
}

func validateHistoricalAcceptance(base *Schema, baseLayer, maxLayer int, operation string) error {
	for _, function := range base.Functions {
		if function.VariantLayer == 0 {
			continue
		}
		for _, interval := range function.AcceptIntervals {
			if interval.MinLayer < baseLayer || interval.MaxLayer > maxLayer {
				return fmt.Errorf("%s: historical function %q accepts %d-%d outside supplied history %d-%d", operation, function.Name, interval.MinLayer, interval.MaxLayer, baseLayer, maxLayer)
			}
		}
	}
	return nil
}

func validateBareLayouts(schema *Schema) error {
	constructorsByFamily := make(map[string][]Constructor)
	for _, constructor := range schema.Constructors {
		constructorsByFamily[constructor.ResultType.FullName()] = append(constructorsByFamily[constructor.ResultType.FullName()], constructor)
	}
	check := func(owner string, reference TypeRef) error {
		var visit func(TypeRef) error
		visit = func(value TypeRef) error {
			if value.IsVector && value.Generic != nil {
				return visit(*value.Generic)
			}
			if !value.IsBare || value.IsBuiltin() || value.IsTypeVar {
				return nil
			}
			constructors := constructorsByFamily[value.FullName()]
			if len(constructors) != 1 {
				return fmt.Errorf("%s: bare reference %s requires exactly one constructor, got %d", owner, value.FullName(), len(constructors))
			}
			return nil
		}
		return visit(reference)
	}
	for _, constructor := range schema.Constructors {
		for _, parameter := range constructor.Params {
			if err := check(constructor.Name+"."+parameter.Name, parameter.Type); err != nil {
				return err
			}
		}
	}
	for _, function := range schema.Functions {
		for _, parameter := range function.Params {
			if err := check(function.Name+"."+parameter.Name, parameter.Type); err != nil {
				return err
			}
		}
		if err := check(function.Name+" result", function.ResultType); err != nil {
			return err
		}
	}
	graph := make(map[string][]string)
	for _, constructor := range schema.Constructors {
		family := constructor.ResultType.FullName()
		for _, parameter := range constructor.Params {
			graph[family] = append(graph[family], bareReferenceNames(parameter.Type)...)
		}
	}
	visiting := make(map[string]bool)
	visited := make(map[string]bool)
	var visitFamily func(string) error
	visitFamily = func(family string) error {
		if visiting[family] {
			return fmt.Errorf("unsupported recursive bare layout involving %s", family)
		}
		if visited[family] {
			return nil
		}
		visiting[family] = true
		for _, child := range graph[family] {
			if err := visitFamily(child); err != nil {
				return err
			}
		}
		delete(visiting, family)
		visited[family] = true
		return nil
	}
	for family := range graph {
		if err := visitFamily(family); err != nil {
			return err
		}
	}
	return nil
}

func bareReferenceNames(reference TypeRef) []string {
	if reference.IsVector && reference.Generic != nil {
		return bareReferenceNames(*reference.Generic)
	}
	if reference.IsBare && !reference.IsBuiltin() && !reference.IsTypeVar {
		return []string{reference.FullName()}
	}
	return nil
}

func differenceLayers(differences []LayerDifference) []int {
	layers := make([]int, len(differences))
	for i := range differences {
		layers[i] = differences[i].Layer
	}
	return layers
}

func contractVariantLayer(all map[string]map[string]int, name, signature string, layer int) int {
	variants := all[name]
	if variants == nil {
		variants = make(map[string]int)
		all[name] = variants
	}
	if variant, ok := variants[signature]; ok {
		return variant
	}
	variant := 0
	if len(variants) != 0 {
		variant = layer
	}
	variants[signature] = variant
	return variant
}

func constructorContractSignature(constructor Constructor) string {
	return semanticKey(struct {
		Name          string
		ID            uint32
		GenericParams []semanticGeneric
		Params        []semanticParameter
		Result        semanticTypeRef
		IsBare        bool
		VectorCount   string
	}{constructor.Name, constructor.ID, semanticGenerics(constructor.GenericParams), semanticParameters(constructor.Params), semanticReference(constructor.ResultType), constructor.IsBare, stringValue(constructor.VectorCount)})
}

func functionRequestSignature(function FuncDecl) string {
	return semanticKey(struct {
		Name          string
		ID            uint32
		GenericParams []semanticGeneric
		Params        []semanticParameter
		IsTemplate    bool
		IsHelper      bool
	}{function.Name, function.ID, semanticGenerics(function.GenericParams), semanticParameters(function.Params), function.IsTemplate, function.IsHelper})
}

func contractTypeRef(reference TypeRef) semanticTypeRef {
	return semanticReference(reference)
}

type semanticGeneric struct{ Name, Constraint string }
type semanticParameter struct {
	Name    string
	Type    semanticTypeRef
	FlagBit *int
}
type semanticTypeRef struct {
	Name, Namespace, GenericArg, FlagName string
	IsVector, IsBare, Optional, IsTypeVar bool
	Generic                               *semanticTypeRef
	FlagBit                               *int
	VariantLayer                          int
}

type resolvedSemanticTypeRef struct {
	Name, Namespace, GenericArg, FlagName string
	IsVector, IsBare, Optional, IsTypeVar bool
	Generic                               *resolvedSemanticTypeRef
	FlagBit                               *int
	BareContract                          string
}

type resolvedSemanticParameter struct {
	Name    string
	Type    resolvedSemanticTypeRef
	FlagBit *int
}

func resolvedTypeContractSignature(name string, schema *Schema, stack map[string]bool) (string, error) {
	if stack == nil {
		stack = make(map[string]bool)
	}
	if stack[name] {
		return "cycle:" + name, nil
	}
	declaration, ok := schema.FindType(name)
	if !ok {
		return "", fmt.Errorf("missing type %s", name)
	}
	stack[name] = true
	defer delete(stack, name)
	type semanticConstructorContract struct {
		Name          string
		ID            uint32
		GenericParams []semanticGeneric
		Params        []resolvedSemanticParameter
		ResultFamily  string
		IsBare        bool
		VectorCount   string
	}
	constructors := make([]semanticConstructorContract, 0, len(declaration.Constructors))
	for _, constructor := range declaration.Constructors {
		params := make([]resolvedSemanticParameter, len(constructor.Params))
		for i, parameter := range constructor.Params {
			reference, err := resolvedSemanticReference(parameter.Type, schema, stack)
			if err != nil {
				return "", err
			}
			params[i] = resolvedSemanticParameter{Name: parameter.Name, Type: reference, FlagBit: cloneIntPointer(parameter.FlagBit)}
		}
		constructors = append(constructors, semanticConstructorContract{
			Name: constructor.Name, ID: constructor.ID, GenericParams: semanticGenerics(constructor.GenericParams),
			Params: params, ResultFamily: constructor.ResultType.FullName(), IsBare: constructor.IsBare,
			VectorCount: stringValue(constructor.VectorCount),
		})
	}
	return semanticKey(constructors), nil
}

func resolvedSemanticReference(value TypeRef, schema *Schema, stack map[string]bool) (resolvedSemanticTypeRef, error) {
	out := resolvedSemanticTypeRef{Name: value.Name, Namespace: value.Namespace, GenericArg: value.GenericArg, FlagName: value.FlagName, IsVector: value.IsVector, IsBare: value.IsBare, Optional: value.Optional, IsTypeVar: value.IsTypeVar, FlagBit: cloneIntPointer(value.FlagBit)}
	if value.Generic != nil {
		generic, err := resolvedSemanticReference(*value.Generic, schema, stack)
		if err != nil {
			return resolvedSemanticTypeRef{}, err
		}
		out.Generic = &generic
	}
	if value.IsBare && !value.IsBuiltin() && !value.IsTypeVar {
		contract, err := resolvedTypeContractSignature(value.FullName(), schema, stack)
		if err != nil {
			return resolvedSemanticTypeRef{}, err
		}
		out.BareContract = contract
	}
	return out, nil
}

func semanticGenerics(values []GenericParam) []semanticGeneric {
	out := make([]semanticGeneric, len(values))
	for i, value := range values {
		out[i] = semanticGeneric{Name: value.Name, Constraint: value.Constraint}
	}
	return out
}

func semanticParameters(values []Parameter) []semanticParameter {
	out := make([]semanticParameter, len(values))
	for i, value := range values {
		out[i] = semanticParameter{Name: value.Name, Type: semanticReference(value.Type), FlagBit: cloneIntPointer(value.FlagBit)}
	}
	return out
}

func semanticReference(value TypeRef) semanticTypeRef {
	out := semanticTypeRef{Name: value.Name, Namespace: value.Namespace, GenericArg: value.GenericArg, FlagName: value.FlagName, IsVector: value.IsVector, IsBare: value.IsBare, Optional: value.Optional, IsTypeVar: value.IsTypeVar, FlagBit: cloneIntPointer(value.FlagBit), VariantLayer: value.VariantLayer}
	if value.Generic != nil {
		generic := semanticReference(*value.Generic)
		out.Generic = &generic
	}
	return out
}

func semanticKey(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("semantic contract key: %v", err))
	}
	return string(encoded)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func annotateBareParameters(parameters []Parameter, versions map[string]int) []Parameter {
	parameters = cloneParameters(parameters)
	for i := range parameters {
		parameters[i].Type = annotateBareTypeRef(parameters[i].Type, versions)
	}
	return parameters
}

func annotateBareTypeRef(reference TypeRef, versions map[string]int) TypeRef {
	reference = cloneTypeRef(reference)
	if reference.Generic != nil {
		generic := annotateBareTypeRef(*reference.Generic, versions)
		reference.Generic = &generic
	}
	if reference.IsBare && !reference.IsBuiltin() && !reference.IsTypeVar {
		reference.VariantLayer = versions[reference.FullName()]
	} else if !reference.IsBare {
		reference.VariantLayer = 0
	}
	return reference
}

func variantKey(name string, layer int) string { return fmt.Sprintf("%s@%d", name, layer) }

func openInterval(intervals *[]LayerInterval, layer int) {
	values := *intervals
	if len(values) == 0 {
		*intervals = []LayerInterval{{MinLayer: layer}}
		return
	}
	last := &values[len(values)-1]
	if last.MaxLayer == 0 || layer <= last.MaxLayer+1 {
		last.MaxLayer = 0
		return
	}
	*intervals = append(values, LayerInterval{MinLayer: layer})
}

func closeInactiveConstructorIntervals(schema *Schema, active map[string]int, current map[string]struct{}, maxLayer int) {
	for key, index := range active {
		if _, ok := current[key]; ok {
			continue
		}
		closeInterval(&schema.Constructors[index].Intervals, maxLayer)
		delete(active, key)
	}
}

func closeInactiveFunctionIntervals(schema *Schema, active map[string]int, current map[string]struct{}, maxLayer int) {
	for key, index := range active {
		if _, ok := current[key]; ok {
			continue
		}
		closeInterval(&schema.Functions[index].Intervals, maxLayer)
		delete(active, key)
	}
}

func closeInterval(intervals *[]LayerInterval, maxLayer int) {
	if len(*intervals) != 0 {
		(*intervals)[len(*intervals)-1].MaxLayer = maxLayer
	}
}

func intersectIntervals(intervals []LayerInterval, minLayer, maxLayer int) []LayerInterval {
	var out []LayerInterval
	for _, interval := range intervals {
		min := interval.MinLayer
		if min < minLayer {
			min = minLayer
		}
		max := interval.MaxLayer
		if max > maxLayer {
			max = maxLayer
		}
		if min <= max {
			out = append(out, LayerInterval{MinLayer: min, MaxLayer: max})
		}
	}
	return out
}

func normalizeIntervals(intervals []LayerInterval) []LayerInterval {
	if len(intervals) == 0 {
		return nil
	}
	out := cloneIntervals(intervals)
	sort.Slice(out, func(i, j int) bool { return out[i].MinLayer < out[j].MinLayer })
	merged := out[:1]
	for _, interval := range out[1:] {
		last := &merged[len(merged)-1]
		if last.MaxLayer == 0 || interval.MinLayer <= last.MaxLayer+1 {
			if last.MaxLayer != 0 && (interval.MaxLayer == 0 || interval.MaxLayer > last.MaxLayer) {
				last.MaxLayer = interval.MaxLayer
			}
			continue
		}
		merged = append(merged, interval)
	}
	return merged
}

func validateLayeredIntervals(schema *Schema) error {
	constructorsByID := make(map[uint32][]Constructor)
	for _, constructor := range schema.Constructors {
		constructorsByID[constructor.ID] = append(constructorsByID[constructor.ID], constructor)
	}
	for id, constructors := range constructorsByID {
		for i := range constructors {
			for j := i + 1; j < len(constructors); j++ {
				if intervalSetsOverlap(constructors[i].Intervals, constructors[j].Intervals) {
					return fmt.Errorf("resolve layers: constructor ID 0x%08x has overlapping contracts", id)
				}
			}
		}
	}
	functionsByID := make(map[uint32][]FuncDecl)
	for _, function := range schema.Functions {
		if !function.IsHelper {
			functionsByID[function.ID] = append(functionsByID[function.ID], function)
		}
	}
	for id, functions := range functionsByID {
		for i := range functions {
			for j := i + 1; j < len(functions); j++ {
				if intervalSetsOverlap(functions[i].Intervals, functions[j].Intervals) {
					return fmt.Errorf("resolve layers: function ID 0x%08x has overlapping contracts", id)
				}
			}
		}
	}
	return nil
}

func intervalSetsOverlap(first, second []LayerInterval) bool {
	for _, left := range first {
		for _, right := range second {
			if layerRangesOverlap(left.MinLayer, left.MaxLayer, right.MinLayer, right.MaxLayer) {
				return true
			}
		}
	}
	return false
}

// Bare references have one statically named concrete codec. Replacing that name
// needs explicit constructor-reference metadata; reject it instead of emitting
// a nonexistent family-derived concrete type.
func validateBareConstructorNames(snapshots []*Schema, historical []FuncDecl) error {
	families := make(map[string]bool)
	collect := func(parameters []Parameter) {
		for _, parameter := range parameters {
			for _, family := range bareReferenceNames(parameter.Type) {
				families[family] = true
			}
		}
	}
	for _, function := range historical {
		collect(function.Params)
	}
	for _, snapshot := range snapshots {
		for _, constructor := range snapshot.Constructors {
			collect(constructor.Params)
		}
		for _, function := range snapshot.Functions {
			collect(function.Params)
		}
	}
	for family := range families {
		name := ""
		for _, snapshot := range snapshots {
			declaration, ok := snapshot.FindType(family)
			if !ok {
				continue
			}
			if len(declaration.Constructors) != 1 {
				return fmt.Errorf("bare family %s requires one constructor at layer %d", family, snapshot.Layer)
			}
			current := declaration.Constructors[0].Name
			if name != "" && name != current {
				return fmt.Errorf("bare family %s: replacing constructor name %s with %s is unsupported", family, name, current)
			}
			name = current
		}
	}
	return nil
}
