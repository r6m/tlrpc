package generator

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/r6m/tlrpc/internal/naming"
	"github.com/r6m/tlrpc/internal/parser"
)

// ProjectionGenerator emits reflection-free, layer-aware output projection for
// one generated package.
type ProjectionGenerator struct {
	namer  *naming.Namer
	out    io.Writer
	schema *parser.Schema
	types  *TypeGenerator
	serial int
	byName map[string][]projectionConcrete
}

type projectionConcrete struct {
	constructor parser.Constructor
	goName      string
}

func (g *ProjectionGenerator) validateTargetRanges(name string, targets []projectionConcrete) error {
	for i := range targets {
		leftMin, leftMax := targets[i].constructor.OutputMinLayer, targets[i].constructor.OutputMaxLayer
		if leftMin == 0 {
			leftMin = g.schema.BaseLayer
		}
		if leftMax == 0 {
			leftMax = g.schema.Layer
		}
		if leftMin > leftMax {
			return fmt.Errorf("generate projection: constructor %q has invalid output range %d..%d", name, leftMin, leftMax)
		}
		for j := i + 1; j < len(targets); j++ {
			rightMin, rightMax := targets[j].constructor.OutputMinLayer, targets[j].constructor.OutputMaxLayer
			if rightMin == 0 {
				rightMin = g.schema.BaseLayer
			}
			if rightMax == 0 {
				rightMax = g.schema.Layer
			}
			if rightMin <= leftMax && leftMin <= rightMax {
				return fmt.Errorf("generate projection: constructor %q has overlapping output ranges for %s and %s", name, targets[i].goName, targets[j].goName)
			}
		}
	}
	return nil
}

// NewProjectionGenerator creates a layer-aware output projection generator.
func NewProjectionGenerator(namer *naming.Namer, out io.Writer) *ProjectionGenerator {
	return &ProjectionGenerator{namer: namer, out: out}
}

// Generate emits ProjectTLObject and TLProjectionHook for a layered schema.
func (g *ProjectionGenerator) Generate(schema *parser.Schema) error {
	if g == nil || g.out == nil {
		return fmt.Errorf("generate projection: output is required")
	}
	if g.namer == nil {
		return fmt.Errorf("generate projection: namer is required")
	}
	if schema == nil {
		return fmt.Errorf("generate projection: schema is required")
	}
	if !schema.IsLayered || schema.BaseLayer <= 0 || schema.Layer < schema.BaseLayer {
		return fmt.Errorf("generate projection: layered schema with a valid base layer is required")
	}
	g.schema = schema
	g.types = NewTypeGenerator(g.namer, io.Discard, schema)
	g.serial = 0
	concretes, err := g.concreteTypes()
	if err != nil {
		return err
	}
	g.byName = make(map[string][]projectionConcrete)
	for _, concrete := range concretes {
		g.byName[concrete.constructor.Name] = append(g.byName[concrete.constructor.Name], concrete)
	}
	for name := range g.byName {
		sort.Slice(g.byName[name], func(i, j int) bool {
			left, right := g.byName[name][i].constructor, g.byName[name][j].constructor
			if left.OutputMinLayer != right.OutputMinLayer {
				return left.OutputMinLayer < right.OutputMinLayer
			}
			return g.byName[name][i].goName < g.byName[name][j].goName
		})
		if err := g.validateTargetRanges(name, g.byName[name]); err != nil {
			return err
		}
	}

	if err := g.writePrelude(schema.BaseLayer, schema.Layer); err != nil {
		return err
	}
	if err := g.writeDispatcher(concretes, schema.Functions); err != nil {
		return err
	}
	return nil
}

func (g *ProjectionGenerator) writePrelude(baseLayer, maxLayer int) error {
	_, err := fmt.Fprintf(g.out, `// TLProjectionHook may replace an object before automatic projection. A handled
// replacement is validated and independently cloned by generated projection with
// the hook skipped for that replacement root. Nested objects still invoke the
// hook. Path contains stable TL constructor names, logical field names, and vector
// indexes.
type TLProjectionHook func(path []string, source tlrpc.TLObject, targetLayer int) (replacement tlrpc.TLObject, handled bool, err error)

const tlProjectionMaxDepth = 128

// ProjectTLObject clones source into the representation supported by targetLayer.
// A zero target layer selects the package's base layer.
func ProjectTLObject(source tlrpc.TLObject, targetLayer int, hook TLProjectionHook) (tlrpc.TLObject, error) {
	if source == nil {
		return nil, fmt.Errorf("project TL object: source is nil")
	}
	if targetLayer == 0 {
		targetLayer = %d
	}
	if targetLayer < %d || targetLayer > %d {
		return nil, fmt.Errorf("project TL object: unsupported target layer %%d (supported %%d..%%d)", targetLayer, %d, %d)
	}
	return projectTLObject(source, targetLayer, hook, []string{tlProjectionSourceName(source)}, 0, true)
}

func tlProjectionPath(path []string) []string {
	return append([]string(nil), path...)
}

func tlProjectionChildPath(path []string, segments ...string) []string {
	next := make([]string, 0, len(path)+len(segments))
	next = append(next, path...)
	next = append(next, segments...)
	return next
}

func tlProjectionError(path []string, format string, args ...interface{}) error {
	return fmt.Errorf("project TL object at %%v: %%s", path, fmt.Sprintf(format, args...))
}

func tlProjectionLayerSupports(layer, minLayer, maxLayer int) bool {
	return (minLayer == 0 || layer >= minLayer) && (maxLayer == 0 || layer <= maxLayer)
}

`, baseLayer, baseLayer, maxLayer, baseLayer, maxLayer)
	return err
}

func (g *ProjectionGenerator) projectionRequests(functions []parser.FuncDecl) []parser.FuncDecl {
	requests := append([]parser.FuncDecl(nil), functions...)
	sort.Slice(requests, func(i, j int) bool { return requestName(g.namer, requests[i]) < requestName(g.namer, requests[j]) })
	result := make([]parser.FuncDecl, 0, len(requests))
	seen := make(map[string]struct{})
	for _, function := range requests {
		if function.IsTemplate || function.IsHelper {
			continue
		}
		name := requestName(g.namer, function)
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, function)
	}
	return result
}

func (g *ProjectionGenerator) writeDispatcher(concretes []projectionConcrete, functions []parser.FuncDecl) error {
	if _, err := io.WriteString(g.out, `func tlProjectionSourceName(source tlrpc.TLObject) string {
	switch source.(type) {
`); err != nil {
		return err
	}
	for _, concrete := range concretes {
		if _, err := fmt.Fprintf(g.out, "\tcase *%s:\n\t\treturn %q\n", concrete.goName, concrete.constructor.Name); err != nil {
			return err
		}
	}
	requests := g.projectionRequests(functions)
	for _, function := range requests {
		name := requestName(g.namer, function)
		if _, err := fmt.Fprintf(g.out, "\tcase *%s:\n\t\treturn %q\n", name, function.Name); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(g.out, `	default:
		return fmt.Sprintf("%T", source)
	}
}

func tlProjectionRejectRequest(source tlrpc.TLObject, path []string) error {
	switch source.(type) {
`); err != nil {
		return err
	}
	for _, function := range requests {
		name := requestName(g.namer, function)
		if _, err := fmt.Fprintf(g.out, "\tcase *%s:\n\t\treturn tlProjectionError(path, \"request input %s cannot be projected as server output\")\n", name, name); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(g.out, `	default:
		return nil
	}
}

func projectTLObject(source tlrpc.TLObject, targetLayer int, hook TLProjectionHook, path []string, depth int, allowHook bool) (tlrpc.TLObject, error) {
	if source == nil {
		return nil, tlProjectionError(path, "source is nil")
	}
	if depth >= tlProjectionMaxDepth {
		return nil, tlProjectionError(path, "maximum projection depth %d exceeded", tlProjectionMaxDepth)
	}
	if err := tlProjectionRejectRequest(source, path); err != nil {
		return nil, err
	}
	if allowHook && hook != nil {
		replacement, handled, err := hook(tlProjectionPath(path), source, targetLayer)
		if err != nil {
			return nil, fmt.Errorf("project TL object at %v: projection hook: %w", path, err)
		}
		if handled {
			if replacement == nil {
				return nil, tlProjectionError(path, "projection hook returned a nil replacement")
			}
			return projectTLObject(replacement, targetLayer, hook, path, depth+1, false)
		}
	}

	switch value := source.(type) {
`); err != nil {
		return err
	}
	for _, concrete := range concretes {
		if _, err := fmt.Fprintf(g.out, "\tcase *%s:\n\t\tif value == nil {\n\t\t\treturn nil, tlProjectionError(path, \"source %s is nil\")\n\t\t}\n", concrete.goName, concrete.goName); err != nil {
			return err
		}
		if err := g.writeConcreteProjection(concrete, "\t\t"); err != nil {
			return err
		}
	}

	_, err := io.WriteString(g.out, `	default:
		return nil, tlProjectionError(path, "unsupported source type %T", source)
	}
}

`)
	return err
}

func (g *ProjectionGenerator) writeConcreteProjection(source projectionConcrete, indent string) error {
	targets := g.byName[source.constructor.Name]
	if len(targets) == 0 {
		_, err := fmt.Fprintf(g.out, "%sreturn nil, tlProjectionError(path, \"constructor %%s has no output projection\", value.TLName())\n", indent)
		return err
	}
	if _, err := fmt.Fprintf(g.out, "%sswitch {\n", indent); err != nil {
		return err
	}
	for _, target := range targets {
		if _, err := fmt.Fprintf(g.out, "%scase tlProjectionLayerSupports(targetLayer, %d, %d):\n", indent, target.constructor.OutputMinLayer, target.constructor.OutputMaxLayer); err != nil {
			return err
		}
		var fields bytes.Buffer
		output := g.out
		g.out = &fields
		terminated, err := g.writeFields(source, target, "value", "projected", indent+"\t")
		g.out = output
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s\tprojected := &%s{}\n", indent, target.goName); err != nil {
			return err
		}
		if terminated {
			if _, err := fmt.Fprintf(g.out, "%s\t_ = projected\n", indent); err != nil {
				return err
			}
		}
		if _, err := io.Copy(g.out, &fields); err != nil {
			return err
		}
		if terminated {
			continue
		}
		if _, err := fmt.Fprintf(g.out, "%s\treturn projected, nil\n", indent); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(g.out, "%sdefault:\n%s\treturn nil, tlProjectionError(path, \"constructor %%s is unavailable at output layer %%d\", value.TLName(), targetLayer)\n%s}\n", indent, indent, indent)
	return err
}

func (g *ProjectionGenerator) writeFields(source, target projectionConcrete, sourceName, targetName, indent string) (bool, error) {
	sourceFields := projectionFields(source.constructor.Params)
	targetFields := projectionFields(target.constructor.Params)
	targetByName := make(map[string]parser.Parameter, len(targetFields))
	for _, field := range targetFields {
		targetByName[field.Name] = field
	}
	sourceByName := make(map[string]parser.Parameter, len(sourceFields))
	for _, field := range sourceFields {
		sourceByName[field.Name] = field
		targetField, exists := targetByName[field.Name]
		fieldExpr := sourceName + "." + g.namer.FieldName(field.Name)
		if !exists {
			terminated, err := g.writeRejectNonzero(field, fieldExpr, field.Name, indent)
			if err != nil {
				return false, err
			}
			if terminated {
				return true, nil
			}
			continue
		}
		if !projectionTypesCompatible(field.Type, targetField.Type) {
			terminated, err := g.writeRejectIncompatible(field, targetField, fieldExpr, field.Name, indent)
			if err != nil {
				return false, err
			}
			if terminated {
				return true, nil
			}
			continue
		}
		if targetField.MinLayer != 0 || targetField.MaxLayer != 0 {
			if _, err := fmt.Fprintf(g.out, "%sif tlProjectionLayerSupports(targetLayer, %d, %d) {\n", indent, targetField.MinLayer, targetField.MaxLayer); err != nil {
				return false, err
			}
			if err := g.writeProjectValue(field, targetField, fieldExpr, targetName+"."+g.namer.FieldName(targetField.Name), field.Name, indent+"\t"); err != nil {
				return false, err
			}
			if _, err := fmt.Fprintf(g.out, "%s} else {\n", indent); err != nil {
				return false, err
			}
			if _, err := g.writeRejectNonzero(field, fieldExpr, field.Name, indent+"\t"); err != nil {
				return false, err
			}
			if _, err := fmt.Fprintf(g.out, "%s}\n", indent); err != nil {
				return false, err
			}
			continue
		}
		if err := g.writeProjectValue(field, targetField, fieldExpr, targetName+"."+g.namer.FieldName(targetField.Name), field.Name, indent); err != nil {
			return false, err
		}
	}
	for _, field := range targetFields {
		if _, exists := sourceByName[field.Name]; exists || projectionOptional(field) {
			continue
		}
		if field.MinLayer != 0 || field.MaxLayer != 0 {
			if _, err := fmt.Fprintf(g.out, "%sif tlProjectionLayerSupports(targetLayer, %d, %d) {\n%s\treturn nil, tlProjectionError(tlProjectionChildPath(path, %q), \"required target field is missing; projection hook must provide it\")\n%s}\n", indent, field.MinLayer, field.MaxLayer, indent, field.Name, indent); err != nil {
				return false, err
			}
			continue
		}
		if _, err := fmt.Fprintf(g.out, "%sreturn nil, tlProjectionError(tlProjectionChildPath(path, %q), \"required target field is missing; projection hook must provide it\")\n", indent, field.Name); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func projectionFields(parameters []parser.Parameter) []parser.Parameter {
	fields := make([]parser.Parameter, 0, len(parameters))
	for _, parameter := range parameters {
		if isFlagParam(parameter) {
			continue
		}
		fields = append(fields, parameter)
	}
	return fields
}

func projectionOptional(parameter parser.Parameter) bool {
	return flagBit(parameter) != nil || parameter.Type.Optional
}

func projectionTypesCompatible(source, target parser.TypeRef) bool {
	if source.IsVector || target.IsVector {
		return source.IsVector && target.IsVector && source.Generic != nil && target.Generic != nil && projectionTypesCompatible(*source.Generic, *target.Generic)
	}
	return source.Name == target.Name && source.Namespace == target.Namespace && source.IsTypeVar == target.IsTypeVar
}

func (g *ProjectionGenerator) writeRejectIncompatible(source, target parser.Parameter, sourceExpr, fieldName, indent string) (bool, error) {
	presence, always := g.nonzeroExpression(source, sourceExpr)
	if always || !projectionOptional(target) {
		_, err := fmt.Fprintf(g.out, "%sreturn nil, tlProjectionError(tlProjectionChildPath(path, %q), \"incompatible field type requires projection hook\")\n", indent, fieldName)
		return true, err
	}
	_, err := fmt.Fprintf(g.out, "%sif %s {\n%s\treturn nil, tlProjectionError(tlProjectionChildPath(path, %q), \"nonzero incompatible field requires projection hook\")\n%s}\n", indent, presence, indent, fieldName, indent)
	return false, err
}

func (g *ProjectionGenerator) writeRejectNonzero(source parser.Parameter, sourceExpr, fieldName, indent string) (bool, error) {
	presence, always := g.nonzeroExpression(source, sourceExpr)
	if always {
		_, err := fmt.Fprintf(g.out, "%sreturn nil, tlProjectionError(tlProjectionChildPath(path, %q), \"unsupported required field requires projection hook\")\n", indent, fieldName)
		return true, err
	}
	_, err := fmt.Fprintf(g.out, "%sif %s {\n%s\treturn nil, tlProjectionError(tlProjectionChildPath(path, %q), \"nonzero unsupported field requires projection hook\")\n%s}\n", indent, presence, indent, fieldName, indent)
	return false, err
}

func (g *ProjectionGenerator) nonzeroExpression(parameter parser.Parameter, expression string) (string, bool) {
	t := parameter.Type
	goType := g.types.goType(t)
	if t.IsVector || t.Name == "bytes" || strings.HasPrefix(goType, "[]") {
		return "len(" + expression + ") != 0", false
	}
	if strings.HasPrefix(goType, "*") || isUnionType(g.schema, t) {
		return expression + " != nil", false
	}
	switch t.Name {
	case "Bool", "bool", "true", "false":
		return expression, false
	case "string":
		return expression + " != \"\"", false
	case "int", "long", "double", "#":
		return expression + " != 0", false
	case "int128", "int256":
		return expression + " != (" + goType + "{})", false
	default:
		return "", true
	}
}

func (g *ProjectionGenerator) writeProjectValue(source, target parser.Parameter, sourceExpr, targetExpr, fieldName, indent string) error {
	pathExpr := fmt.Sprintf("tlProjectionChildPath(path, %q)", fieldName)
	return g.writeProjectType(source.Type, target.Type, sourceExpr, targetExpr, pathExpr, fieldName, indent, false)
}

func (g *ProjectionGenerator) writeProjectType(source, target parser.TypeRef, sourceExpr, targetExpr, pathExpr, fieldName, indent string, vectorElement bool) error {
	if source.IsVector && target.IsVector && source.Generic != nil && target.Generic != nil {
		return g.writeProjectVector(source, target, sourceExpr, targetExpr, pathExpr, fieldName, indent)
	}
	if g.projectionObjectType(source) && g.projectionObjectType(target) {
		return g.writeProjectObject(source, target, sourceExpr, targetExpr, pathExpr, fieldName, indent, vectorElement)
	}
	if source.Name == "bytes" && target.Name == "bytes" {
		_, err := fmt.Fprintf(g.out, "%s%s = append([]byte(nil), %s...)\n", indent, targetExpr, sourceExpr)
		return err
	}
	return g.writeProjectScalar(source, target, sourceExpr, targetExpr, fieldName, indent)
}

func (g *ProjectionGenerator) writeProjectScalar(source, target parser.TypeRef, sourceExpr, targetExpr, fieldName, indent string) error {
	sourceType := g.types.goType(source)
	targetType := g.types.goType(target)
	sourcePointer := strings.HasPrefix(sourceType, "*")
	targetPointer := strings.HasPrefix(targetType, "*")
	switch {
	case sourcePointer && targetPointer:
		name := g.next("value")
		_, err := fmt.Fprintf(g.out, "%sif %s != nil {\n%s\t%s := *%s\n%s\t%s = &%s\n%s}\n", indent, sourceExpr, indent, name, sourceExpr, indent, targetExpr, name, indent)
		return err
	case sourcePointer:
		if _, err := fmt.Fprintf(g.out, "%sif %s == nil {\n%s\treturn nil, tlProjectionError(tlProjectionChildPath(path, %q), \"required target field is missing\")\n%s}\n", indent, sourceExpr, indent, fieldName, indent); err != nil {
			return err
		}
		_, err := fmt.Fprintf(g.out, "%s%s = *%s\n", indent, targetExpr, sourceExpr)
		return err
	case targetPointer:
		name := g.next("value")
		_, err := fmt.Fprintf(g.out, "%s%s := %s\n%s%s = &%s\n", indent, name, sourceExpr, indent, targetExpr, name)
		return err
	default:
		_, err := fmt.Fprintf(g.out, "%s%s = %s\n", indent, targetExpr, sourceExpr)
		return err
	}
}

func (g *ProjectionGenerator) writeProjectVector(source, target parser.TypeRef, sourceExpr, targetExpr, pathExpr, fieldName, indent string) error {
	targetType := g.types.goType(target)
	index := g.next("index")
	if _, err := fmt.Fprintf(g.out, "%sif %s != nil {\n%s\t%s = make(%s, len(%s))\n%s\tfor %s := range %s {\n", indent, sourceExpr, indent, targetExpr, targetType, sourceExpr, indent, index, sourceExpr); err != nil {
		return err
	}
	elementPath := g.next("elementPath")
	if _, err := fmt.Fprintf(g.out, "%s\t\t%s := tlProjectionChildPath(%s, fmt.Sprintf(\"[%%d]\", %s))\n", indent, elementPath, pathExpr, index); err != nil {
		return err
	}
	if err := g.writeProjectType(*source.Generic, *target.Generic, sourceExpr+"["+index+"]", targetExpr+"["+index+"]", elementPath, fieldName, indent+"\t\t", true); err != nil {
		return err
	}
	_, err := fmt.Fprintf(g.out, "%s\t\t_ = %s\n%s\t}\n%s}\n", indent, elementPath, indent, indent)
	return err
}

func (g *ProjectionGenerator) writeProjectObject(source, target parser.TypeRef, sourceExpr, targetExpr, pathExpr, fieldName, indent string, vectorElement bool) error {
	sourceType := g.projectionValueType(source, vectorElement)
	targetType := g.projectionValueType(target, vectorElement)
	sourceNullable := strings.HasPrefix(sourceType, "*") || isUnionType(g.schema, source)
	targetNullable := strings.HasPrefix(targetType, "*") || isUnionType(g.schema, target)
	objectExpr := sourceExpr
	if !sourceNullable {
		objectExpr = "&" + sourceExpr
	}
	writeBody := func(bodyIndent string) error {
		projectedName := g.next("object")
		mappedName := g.next("mapped")
		okName := g.next("ok")
		objectPath := g.next("objectPath")
		if _, err := fmt.Fprintf(g.out, "%s%s := tlProjectionChildPath(%s, tlProjectionSourceName(%s))\n", bodyIndent, objectPath, pathExpr, objectExpr); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "%s%s, err := projectTLObject(%s, targetLayer, hook, %s, depth+1, true)\n%sif err != nil {\n%s\treturn nil, err\n%s}\n", bodyIndent, projectedName, objectExpr, objectPath, bodyIndent, bodyIndent, bodyIndent); err != nil {
			return err
		}
		assertType := targetType
		if !targetNullable {
			assertType = "*" + targetType
		}
		if _, err := fmt.Fprintf(g.out, "%s%s, %s := %s.(%s)\n%sif !%s {\n%s\treturn nil, tlProjectionError(%s, \"projected field has type %%T, want %s\", %s)\n%s}\n", bodyIndent, mappedName, okName, projectedName, assertType, bodyIndent, okName, bodyIndent, objectPath, assertType, projectedName, bodyIndent); err != nil {
			return err
		}
		if targetNullable {
			_, err := fmt.Fprintf(g.out, "%s%s = %s\n", bodyIndent, targetExpr, mappedName)
			return err
		}
		_, err := fmt.Fprintf(g.out, "%s%s = *%s\n", bodyIndent, targetExpr, mappedName)
		return err
	}
	if sourceNullable {
		if _, err := fmt.Fprintf(g.out, "%sif %s != nil {\n", indent, sourceExpr); err != nil {
			return err
		}
		if err := writeBody(indent + "\t"); err != nil {
			return err
		}
		if !projectionTypeOptional(target) {
			if _, err := fmt.Fprintf(g.out, "%s} else {\n%s\treturn nil, tlProjectionError(tlProjectionChildPath(path, %q), \"required target field is missing\")\n%s}\n", indent, indent, fieldName, indent); err != nil {
				return err
			}
			return nil
		}
		_, err := fmt.Fprintf(g.out, "%s}\n", indent)
		return err
	}
	return writeBody(indent)
}

func projectionTypeOptional(t parser.TypeRef) bool {
	return t.Optional || t.FlagBit != nil
}

func (g *ProjectionGenerator) projectionObjectType(t parser.TypeRef) bool {
	return !t.IsVector && !isBuiltinTLType(t.Name) && !t.IsTypeVar
}

func (g *ProjectionGenerator) projectionValueType(t parser.TypeRef, vectorElement bool) string {
	if vectorElement {
		if isUnionType(g.schema, t) {
			return unionInterfaceName(g.namer, t)
		}
		if shouldUsePointerForType(g.schema, t) {
			return "*" + g.types.goBaseTypeNonVector(t)
		}
		return g.types.goBaseTypeNonVector(t)
	}
	return g.types.goType(t)
}

func (g *ProjectionGenerator) next(prefix string) string {
	g.serial++
	return fmt.Sprintf("%s%d", prefix, g.serial)
}

func (g *ProjectionGenerator) concreteTypes() ([]projectionConcrete, error) {
	var result []projectionConcrete
	seen := make(map[string]struct{})
	for i := range g.schema.Types {
		declaration := &g.schema.Types[i]
		for j := range declaration.Constructors {
			constructor := declaration.Constructors[j]
			if len(constructor.GenericParams) != 0 || constructor.ResultType.IsTypeVar || g.types.isBaseType(&constructor) {
				continue
			}
			goName := constructorName(g.namer, constructor)
			if len(declaration.Constructors) == 1 {
				goName = typeName(g.namer, declaration.Name, declaration.VariantLayer)
			}
			if _, exists := seen[goName]; exists {
				continue
			}
			seen[goName] = struct{}{}
			result = append(result, projectionConcrete{constructor: constructor, goName: goName})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].goName < result[j].goName })
	return result, nil
}
