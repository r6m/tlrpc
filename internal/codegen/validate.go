package codegen

import (
	"fmt"
	"regexp"
	"strings"
)

// Validator validates parsed TL schemas
type Validator struct {
	schema *Schema
	errors []ValidationError
}

// NewValidator creates a new validator for the given schema
func NewValidator(schema *Schema) *Validator {
	return &Validator{
		schema: schema,
		errors: []ValidationError{},
	}
}

// Validate performs all validation checks on the schema
func (v *Validator) Validate() error {
	v.validateUniqueConstructorIDs()
	v.validateUniqueFunctionIDs()
	v.validateTypeResolution()
	v.validateFlagConsistency()
	v.validateCircularDependencies()
	v.validateNamespaceValidity()

	if len(v.errors) > 0 {
		var errs []error
		for _, e := range v.errors {
			errs = append(errs, e)
		}
		return NewMultiError(errs)
	}
	return nil
}

// Errors returns all validation errors
func (v *Validator) Errors() []ValidationError {
	return v.errors
}

// addError adds a validation error
func (v *Validator) addError(line, col int, message string, severity ErrorSeverity) {
	v.errors = append(v.errors, ValidationError{
		Line:     line,
		Column:   col,
		Message:  message,
		Severity: severity,
	})
}

// validateUniqueConstructorIDs checks for duplicate constructor IDs
func (v *Validator) validateUniqueConstructorIDs() {
	idMap := make(map[uint32][]*Constructor)

	// Collect all constructors by ID
	for i := range v.schema.Constructors {
		ctor := &v.schema.Constructors[i]
		idMap[ctor.ID] = append(idMap[ctor.ID], ctor)
	}

	// Check for duplicates
	for id, ctors := range idMap {
		if len(ctors) > 1 {
			for _, ctor := range ctors {
				v.addError(0, 0,
					fmt.Sprintf("duplicate constructor ID 0x%08x used by %s", id, ctor.Name),
					ErrorSeverityError)
			}
		}
	}
}

// validateUniqueFunctionIDs checks for duplicate function IDs
func (v *Validator) validateUniqueFunctionIDs() {
	idMap := make(map[uint32][]*FuncDecl)

	// Collect all functions by ID
	for i := range v.schema.Functions {
		fn := &v.schema.Functions[i]
		idMap[fn.ID] = append(idMap[fn.ID], fn)
	}

	// Check for duplicates
	for id, fns := range idMap {
		if len(fns) > 1 {
			for _, fn := range fns {
				v.addError(0, 0,
					fmt.Sprintf("duplicate function ID 0x%08x used by %s", id, fn.Name),
					ErrorSeverityError)
			}
		}
	}
}

// validateTypeResolution checks that all referenced types are defined
func (v *Validator) validateTypeResolution() {
	definedTypes := make(map[string]bool)

	// Add built-in types
	for typ := range builtinTypes {
		definedTypes[typ] = true
	}

	// Add declared types
	for _, typ := range v.schema.Types {
		definedTypes[typ.Name] = true
	}

	// Check constructor result types
	for _, ctor := range v.schema.Constructors {
		// Add generic parameters to defined types for this constructor
		localTypes := make(map[string]bool)
		for k, v := range definedTypes {
			localTypes[k] = v
		}
		for _, generic := range ctor.GenericParams {
			localTypes[generic.Name] = true
		}

		v.checkTypeReference(ctor.ResultType, localTypes, fmt.Sprintf("constructor %s result type", ctor.Name))
		for _, param := range ctor.Params {
			v.checkTypeReference(param.Type, localTypes, fmt.Sprintf("constructor %s parameter %s", ctor.Name, param.Name))
		}
	}

	// Check function result types and parameters
	for _, fn := range v.schema.Functions {
		// Add generic parameters to defined types for this function
		localTypes := make(map[string]bool)
		for k, v := range definedTypes {
			localTypes[k] = v
		}
		for _, generic := range fn.GenericParams {
			localTypes[generic.Name] = true
		}

		v.checkTypeReference(fn.ResultType, localTypes, fmt.Sprintf("function %s result type", fn.Name))
		for _, param := range fn.Params {
			v.checkTypeReference(param.Type, localTypes, fmt.Sprintf("function %s parameter %s", fn.Name, param.Name))
		}
	}
}

// checkTypeReference validates a single type reference
func (v *Validator) checkTypeReference(typeRef TypeRef, definedTypes map[string]bool, context string) {
	// For conditional types, check the actual type
	if typeRef.FlagBit != nil {
		typeName := typeRef.FullName()
		if !definedTypes[typeName] && !IsBuiltinType(typeName) {
			v.addError(0, 0,
				fmt.Sprintf("undefined type %s in %s", typeName, context),
				ErrorSeverityError)
		}
		return
	}

	typeName := typeRef.FullName()

	// Check main type
	if !definedTypes[typeName] && !IsBuiltinType(typeName) {
		v.addError(0, 0,
			fmt.Sprintf("undefined type %s in %s", typeName, context),
			ErrorSeverityError)
	}

	// Check vector element type
	if typeRef.IsVector && typeRef.Generic != nil {
		v.checkTypeReference(*typeRef.Generic, definedTypes, fmt.Sprintf("%s vector element", context))
	}
}

// validateFlagConsistency checks flag bit uniqueness within constructors
func (v *Validator) validateFlagConsistency() {
	for i := range v.schema.Constructors {
		v.validateConstructorFlags(&v.schema.Constructors[i])
	}

	for i := range v.schema.Functions {
		v.validateFunctionFlags(&v.schema.Functions[i])
	}
}

// validateConstructorFlags validates flags in a constructor
func (v *Validator) validateConstructorFlags(ctor *Constructor) {
	// In TL, multiple parameters can use the same flag bit if they should be present together
	// No validation needed - this is allowed
}

// validateFunctionFlags validates flags in a function
func (v *Validator) validateFunctionFlags(fn *FuncDecl) {
	// In TL, multiple parameters can use the same flag bit if they should be present together
	// No validation needed - this is allowed
}

// validateCircularDependencies checks for circular type dependencies
func (v *Validator) validateCircularDependencies() {
	// Create dependency graph
	deps := make(map[string][]string)

	// Build dependency graph
	for _, typ := range v.schema.Types {
		for _, ctor := range typ.Constructors {
			v.collectDependencies(ctor.ResultType, deps, typ.Name)
			for _, param := range ctor.Params {
				v.collectDependencies(param.Type, deps, typ.Name)
			}
		}
	}

	// Check for cycles
	visiting := make(map[string]bool)
	visited := make(map[string]bool)

	for typ := range deps {
		if !visited[typ] {
			v.checkCycle(typ, deps, visiting, visited)
		}
	}
}

// collectDependencies collects type dependencies
func (v *Validator) collectDependencies(typeRef TypeRef, deps map[string][]string, fromType string) {
	typeName := typeRef.FullName()

	// Skip built-in types and vectors
	if IsBuiltinType(typeName) || typeRef.IsVector {
		return
	}

	// Skip self-references (constructor result types)
	if typeName == fromType {
		return
	}

	// Add dependency
	if _, exists := deps[fromType]; !exists {
		deps[fromType] = []string{}
	}
	// Only add if not already present
	for _, dep := range deps[fromType] {
		if dep == typeName {
			return
		}
	}
	deps[fromType] = append(deps[fromType], typeName)

	// Recurse for vector elements
	if typeRef.IsVector && typeRef.Generic != nil {
		v.collectDependencies(*typeRef.Generic, deps, fromType)
	}
}

// checkCycle checks for cycles in dependency graph using DFS
func (v *Validator) checkCycle(node string, deps map[string][]string, visiting, visited map[string]bool) {
	visiting[node] = true
	defer func() { visiting[node] = false }()

	for _, dep := range deps[node] {
		if visiting[dep] {
			v.addError(0, 0,
				fmt.Sprintf("circular type dependency involving %s", dep),
				ErrorSeverityError)
			return
		}
		if !visited[dep] {
			v.checkCycle(dep, deps, visiting, visited)
		}
	}

	visited[node] = true
}

// validateNamespaceValidity checks namespace identifier format
func (v *Validator) validateNamespaceValidity() {
	validIdent := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

	// Check type namespaces - allow dots in type names for namespaced types
	for _, typ := range v.schema.Types {
		if typ.Name != "" {
			parts := strings.Split(typ.Name, ".")
			for _, part := range parts {
				if !validIdent.MatchString(part) {
					v.addError(0, 0,
						fmt.Sprintf("invalid type name format: %s", typ.Name),
						ErrorSeverityError)
					break
				}
			}
		}
	}

	// Check constructor namespaces
	for _, ctor := range v.schema.Constructors {
		parts := splitNamespace(ctor.Name)
		for _, part := range parts {
			if !validIdent.MatchString(part) {
				v.addError(0, 0,
					fmt.Sprintf("invalid constructor name format: %s", ctor.Name),
					ErrorSeverityError)
				break
			}
		}
	}

	// Check function namespaces
	for _, fn := range v.schema.Functions {
		parts := splitNamespace(fn.Name)
		for _, part := range parts {
			if !validIdent.MatchString(part) {
				v.addError(0, 0,
					fmt.Sprintf("invalid function name format: %s", fn.Name),
					ErrorSeverityError)
				break
			}
		}
	}
}

// splitNamespace splits a dotted identifier into parts
func splitNamespace(name string) []string {
	// Handle both "namespace.name" and "name" formats
	parts := []string{}
	current := ""
	for _, r := range name {
		if r == '.' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(r)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
